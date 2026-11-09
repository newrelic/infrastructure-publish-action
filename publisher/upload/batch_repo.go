package upload

import (
	"fmt"
	"io/ioutil"
	"net/http"
	"os"
	"path"
	"strings"
	"time"

	"github.com/newrelic/infrastructure-publish-action/publisher/config"
	"github.com/newrelic/infrastructure-publish-action/publisher/download"
	"github.com/newrelic/infrastructure-publish-action/publisher/utils"
)

// Repository steps used by batch mode (batch.go). They are the same steps uploadApt and uploadRpm
// run for a single app, split so that a batch runs each repository step once for all its apps.
// The single-app path (upload.go) is left as it is.

// rpmFile holds the paths for one rpm package going into one yum/zypp repository.
type rpmFile struct {
	srcPath    string // downloaded package
	destPath   string // repository path relative to the bucket root
	s3RepoPath string // repository path in the mounted bucket
	rpmPath    string // package path in the mounted bucket
	override   bool
}

func newRpmFile(conf config.Config, srcTemplate string, uploadConf config.Upload, arch, osVersion string) rpmFile {
	// RPM specific architecture variables
	destPathArch := arch
	switch destPathArch {
	case "arm64":
		destPathArch = "aarch64"
	}

	downloadedRpmFileName := download.GenerateDownloadFileName(
		srcTemplate,
		conf.RepoName,
		conf.AppName,
		arch,
		conf.Tag,
		conf.Version,
		conf.DestPrefix,
		osVersion)

	destPath := generateDestinationAssetsPath(
		downloadedRpmFileName,
		uploadConf.Dest,
		conf.RepoName,
		conf.AppName,
		destPathArch,
		conf.Tag,
		conf.Version,
		conf.DestPrefix,
		osVersion)

	s3RepoPath := path.Join(conf.ArtifactsDestFolder, destPath)

	return rpmFile{
		srcPath:    path.Join(conf.ArtifactsSrcFolder, downloadedRpmFileName),
		destPath:   destPath,
		s3RepoPath: s3RepoPath,
		rpmPath:    path.Join(s3RepoPath, downloadedRpmFileName),
		override:   uploadConf.Override,
	}
}

func copyRpmFile(rpm rpmFile) error {
	return utils.CopyFile(rpm.srcPath, rpm.rpmPath, rpm.override, commandTimeout)
}

// updateRpmRepo creates the repository if missing, writes the .repo file, regenerates the
// metadata for every package present in s3RepoPath and signs it. The new metadata is checked against the
// repository before it replaces the old one (see checkRpmIndex): when it is incomplete, e.g. because the S3
// mount dropped while createrepo was reading, the bucket is remounted and the repository is built again.
func updateRpmRepo(conf config.Config, s3RepoPath, destPath, source string) error {
	attempt := 0
	return retryBadIndex(rpmIndexTries,
		func() error {
			attempt++
			return updateRpmRepoOnce(conf, s3RepoPath, destPath, source, attempt)
		},
		func() { _ = os.RemoveAll(path.Join(os.TempDir(), "repodata")) },
		func() error { return forceRemount(conf.ArtifactsDestFolder) })
}

func updateRpmRepoOnce(conf config.Config, s3RepoPath, destPath, source string, attempt int) (err error) {
	s3DotRepoFilepath := path.Join(s3RepoPath, "newrelic-infra.repo")
	s3RepoData := path.Join(s3RepoPath, "repodata")
	s3RepomdFilepath := path.Join(s3RepoPath, repodataRpmPath)
	signaturePath := path.Join(s3RepoPath, signatureRpmPath)

	// check for repo and create if missing
	if _, err = os.Stat(s3RepomdFilepath); os.IsNotExist(err) {

		utils.Logger.Printf("[ ] Didn't find repo for %s, run repo init command", s3RepoPath)

		if err := utils.ExecLogOutput(utils.Logger, "createrepo", commandTimeout, s3RepoPath, "-o", os.TempDir()); err != nil {
			return err
		}

		utils.Logger.Printf("[✔] Repo created: %s", s3RepoPath)
	} else {
		_ = os.Remove(signaturePath)
	}

	// create .repo file
	utils.Logger.Println(fmt.Sprintf("creating 'newrelic-infra.repo' file in %s", s3RepoPath))
	repoFileContent := generateRepoFileContent(conf.AccessPointHost, destPath)
	err = ioutil.WriteFile(s3DotRepoFilepath, []byte(repoFileContent), 0644)
	if err != nil {
		return err
	}

	// "cache" the repodata from s3 to local so it doesnt have to process all again
	if _, err = os.Stat(s3RepoData + "/"); err == nil {
		if err = utils.ExecWithRetries(s3Retries, utils.S3RemountFn, utils.Logger, "cp", commandTimeout, "-rf", s3RepoData+"/", os.TempDir()+"/repodata/"); err != nil {
			return err
		}
	}

	tmpRepodata := path.Join(os.TempDir(), "repodata")
	previouslyIndexed := 0
	if n, countErr := indexedPackages(tmpRepodata); countErr == nil {
		previouslyIndexed = n
	}

	started := time.Now()
	output, err := runCreaterepo(createrepoUpdateArgs(s3RepoPath, os.TempDir())...)
	utils.Logger.Printf("[batch] attempt %d: createrepo took %s", attempt, time.Since(started).Round(time.Second))
	if err != nil {
		diagnoseIndex(s3RepoPath, tmpRepodata)
		return fmt.Errorf("%w: createrepo failed: %v", errBadIndex, err)
	}
	// createrepo exits successfully even when it could not read some packages: do not replace the
	// metadata in s3 unless it lists every package of the repository
	if err = checkRpmIndex(s3RepoPath, tmpRepodata, previouslyIndexed, output); err != nil {
		diagnoseIndex(s3RepoPath, tmpRepodata)
		return err
	}

	// remove the 'old' repodata from s3
	if err = utils.ExecWithRetries(s3Retries, utils.S3RemountFn, utils.Logger, "rm", commandTimeout, "-rf", s3RepoData+"/"); err != nil {
		return err
	}

	// copy from temp repodata to repo repodata in s3
	if err = utils.ExecWithRetries(s3Retries, utils.S3RemountFn, utils.Logger, "cp", commandTimeout, "-rf", os.TempDir()+"/repodata/", s3RepoPath); err != nil {
		return err
	}

	// remove temp repodata so the next repo doesn't get confused
	if err = utils.ExecWithRetries(s3Retries, utils.S3RemountFn, utils.Logger, "rm", commandTimeout, "-rf", os.TempDir()+"/repodata/"); err != nil {
		return err
	}

	// verify that metadata copied to s3
	if _, err = os.Stat(s3RepomdFilepath); err != nil {
		return fmt.Errorf("error while creating repository %s for source %s and destination %s", err.Error(), source, destPath)
	}

	// sign metadata with GPG key
	if err := utils.ExecLogOutput(utils.Logger, "gpg", commandTimeout, "--batch", "--pinentry-mode=loopback", "--passphrase", conf.GpgPassphrase, "--keyring", conf.GpgKeyRing, "--detach-sign", "--armor", s3RepomdFilepath); err != nil {
		return err
	}

	return nil
}

// published in the source repo (unless mirroring is skipped).
func createAptRepo(conf config.Config, upload config.Upload, osVersion string) (err error) {
	utils.Logger.Printf("[ ] Create local repo for os %s", osVersion)
	// aptly repo create --distribution=${DISTRO} ${DISTRO}
	if err = utils.ExecLogOutput(utils.Logger, "aptly", commandTimeout, "repo", "create", "--distribution="+osVersion, osVersion); err != nil {
		return err
	}
	utils.Logger.Printf("[✔] Local repo created for os %s", osVersion)

	if !conf.AptSkipMirror {
		// Mirror repo start
		srcRepo := batchAptSrcRepo(conf, upload, osVersion)
		exists, existsErr := aptDistExists(srcRepo, osVersion)
		if existsErr != nil {
			return fmt.Errorf("cannot check the apt repo to mirror %s: %w (set apt_skip_mirror to start an empty repo)", srcRepo, existsErr)
		}
		if !exists {
			// first publish under this prefix: nothing to mirror, the batch's packages are the whole repo
			utils.Logger.Printf("[ ] No apt repo for %s at %s yet, starting an empty one", osVersion, srcRepo)
			return nil
		}
		err = mirrorAPTRepo(conf, srcRepo, osVersion)
		if err != nil {
			return err
		}
	}
	return nil
}

// batchAptSrcRepo is the apt repo mirrored before the batch adds its packages. It is the repo the batch
// publishes to (mirror host + rendered dest), so a prefix other than infrastructure_agent/ is
// updated in place and does not pull the packages of another one. The schema's src_repo is not used.
func batchAptSrcRepo(conf config.Config, upload config.Upload, osVersion string) string {
	dest := utils.ReplacePlaceholders(upload.Dest, "", "", "", "", "", conf.DestPrefix, osVersion)
	return strings.TrimSuffix(conf.MirrorHost, "/") + "/" + strings.Trim(dest, "/")
}

// aptDistExists tells whether srcRepo already has the distribution. Only a 404 means "not there yet";
// any other answer (403, 5xx, network error) is an error, so a misread never publishes an empty index
// over an existing repo. It is a variable for tests.
var aptDistExists = func(srcRepo, osVersion string) (bool, error) {
	client := http.Client{Timeout: 30 * time.Second}
	resp, err := client.Head(srcRepo + "/dists/" + osVersion + "/Release")
	if err != nil {
		return false, err
	}
	defer resp.Body.Close()
	switch resp.StatusCode {
	case http.StatusOK:
		return true, nil
	case http.StatusNotFound:
		return false, nil
	default:
		return false, fmt.Errorf("unexpected status %d", resp.StatusCode)
	}
}

// addAptPackage adds one package into the local aptly repo and copies it into the pool.
// It returns the dists path the metadata has to be synced to.
func addAptPackage(conf config.Config, srcTemplate string, upload config.Upload, arch, osVersion string) (destPath string, err error) {
	fileName, dest := replaceSrcDestTemplates(
		srcTemplate,
		upload.Dest,
		conf.RepoName,
		conf.AppName,
		arch,
		conf.Tag,
		conf.Version,
		conf.DestPrefix,
		osVersion)

	srcPath := path.Join(conf.ArtifactsSrcFolder, fileName)
	destPath = path.Join(conf.ArtifactsDestFolder, dest, aptDists)
	// path where the package will be located expected by aplty (write metadata with this path)
	filePath := path.Join(conf.ArtifactsDestFolder, dest, aptPoolMain, string(fileName[0]), "/", conf.AppName, fileName)

	utils.Logger.Printf("[ ] Add package %s into deb repo for %s/%s", srcPath, osVersion, arch)
	if err = utils.ExecLogOutput(utils.Logger, "aptly", commandTimeout, "repo", "add", "-force-replace=true", osVersion, srcPath); err != nil {
		return "", err
	}
	utils.Logger.Printf("[✔] Added successfully package into deb repo for %s/%s", osVersion, arch)

	// Create the directory and copy the binary
	if err = utils.ExecWithRetries(s3Retries, utils.S3RemountFn, utils.Logger, "mkdir", commandTimeout, "-p", path.Dir(filePath)); err != nil {
		return "", err
	}
	if err = utils.ExecWithRetries(s3Retries, utils.S3RemountFn, utils.Logger, "cp", commandTimeout, "-f", srcPath, filePath); err != nil {
		return "", err
	}
	return destPath, nil
}

// publishAptRepo signs and publishes the local aptly repo and syncs its metadata into destPath.
func publishAptRepo(conf config.Config, destPath, osVersion string) (err error) {
	utils.Logger.Printf("[ ] Publish deb repo for %s", osVersion)
	if err = utils.ExecLogOutput(utils.Logger, "aptly", commandTimeout, "publish", "repo", "-origin=New Relic", "-keyring", conf.GpgKeyRing, "-passphrase", conf.GpgPassphrase, "-batch", osVersion); err != nil {
		return err
	}

	utils.Logger.Printf("[✔] Published successfully deb repo for %s", osVersion)
	if err = syncAPTMetadata(conf, destPath, osVersion); err != nil {
		return err
	}
	utils.Logger.Printf("[✔] Synced successfully local repo for %s into s3", osVersion)
	return nil
}
