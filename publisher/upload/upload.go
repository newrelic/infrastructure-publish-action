package upload

import (
	"fmt"
	"github.com/newrelic/infrastructure-publish-action/publisher/config"
	"github.com/newrelic/infrastructure-publish-action/publisher/download"
	"github.com/newrelic/infrastructure-publish-action/publisher/lock"
	"github.com/newrelic/infrastructure-publish-action/publisher/utils"
	"io/ioutil"
	"net/http"
	"net/url"
	"os"
	"path"
	"strings"
	"time"
)

const (
	//FileTypes
	typeFile         = "file"
	typeZypp         = "zypp"
	typeYum          = "yum"
	typeApt          = "apt"
	repodataRpmPath  = "/repodata/repomd.xml"
	signatureRpmPath = "/repodata/repomd.xml.asc"
	aptPoolMain      = "pool/main/"
	aptDists         = "dists/"
	commandTimeout   = time.Hour * 1

	s3Retries = 10
)

func uploadArtifact(conf config.Config, schema config.UploadArtifactSchema, upload config.Upload) (err error) {
	if upload.Type == typeFile {
		utils.Logger.Println("Uploading file artifact")
		for _, arch := range schema.Arch {
			err = uploadFileArtifact(conf, schema, upload, arch)
			if err != nil {
				return err
			}
		}
	} else if upload.Type == typeYum || upload.Type == typeZypp {
		utils.Logger.Println("Uploading rpm as yum or zypp")
		for _, arch := range schema.Arch {
			err = uploadRpm(conf, schema.Src, upload, arch)
			if err != nil {
				return err
			}
		}
	} else if upload.Type == typeApt {
		utils.Logger.Println("Uploading apt")
		err = uploadApt(conf, schema.Src, upload, schema.Arch)
		if err != nil {
			return err
		}
	}

	return nil
}

func UploadArtifacts(conf config.Config, schema config.UploadArtifactSchemas, bucketLock lock.BucketLock) (err error) {
	if err = bucketLock.Lock(); err != nil {
		return
	}
	defer func() {
		errRelease := bucketLock.Release()
		if err == nil {
			err = errRelease
		} else if errRelease != nil {
			err = fmt.Errorf("got 2 errors: uploading: \"%v\", releasing lock: \"%v\"", err, errRelease)
		}
		return
	}()

	for _, artifactSchema := range schema {
		for _, upload := range artifactSchema.Uploads {
			err := uploadArtifact(conf, artifactSchema, upload)
			if err != nil {
				return err
			}

		}
	}
	return nil
}

func uploadRpm(conf config.Config, srcTemplate string, uploadConf config.Upload, arch string) (err error) {

	// RPM specific architecture variables
	destPathArch := arch
	switch destPathArch {
	case "arm64":
		destPathArch = "aarch64"
	}

	for _, osVersion := range uploadConf.OsVersion {
		utils.Logger.Printf("[ ] Start uploading rpm for os %s/%s", osVersion, arch)

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

		downloadedRpmFilePath := path.Join(conf.ArtifactsSrcFolder, downloadedRpmFileName)
		s3RepoPath := path.Join(conf.ArtifactsDestFolder, destPath)
		s3DotRepoFilepath := path.Join(s3RepoPath, "newrelic-infra.repo")
		s3RepoData := path.Join(s3RepoPath, "repodata")
		rpmDestinationPath := path.Join(s3RepoPath, downloadedRpmFileName)
		s3RepomdFilepath := path.Join(s3RepoPath, repodataRpmPath)
		signaturePath := path.Join(s3RepoPath, signatureRpmPath)

		// copy rpm file to be able to add it into the index later
		err = utils.CopyFile(downloadedRpmFilePath, rpmDestinationPath, uploadConf.Override, commandTimeout)
		if err != nil {
			return err
		}

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

		if err = utils.ExecLogOutput(utils.Logger, "createrepo", commandTimeout, "--update", "-s", "sha", s3RepoPath, "-o", os.TempDir()); err != nil {
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
			return fmt.Errorf("error while creating repository %s for source %s and destination %s", err.Error(), downloadedRpmFilePath, destPath)
		}

		// sign metadata with GPG key
		if err := utils.ExecLogOutput(utils.Logger, "gpg", commandTimeout, "--batch", "--pinentry-mode=loopback", "--passphrase", conf.GpgPassphrase, "--keyring", conf.GpgKeyRing, "--detach-sign", "--armor", s3RepomdFilepath); err != nil {
			return err
		}

		utils.Logger.Printf("[✔] Uploading RPM succeded for src %s and dest %s \n", downloadedRpmFilePath, destPath)
	}

	return nil
}

func uploadApt(conf config.Config, srcTemplate string, upload config.Upload, archs []string) (err error) {

	// the dest path for apt is the same for each distribution since it does not depend on it
	var destPath string
	for _, osVersion := range upload.OsVersion {
		utils.Logger.Printf("[ ] Start uploading deb for os %s", osVersion)

		utils.Logger.Printf("[ ] Create local repo for os %s", osVersion)
		// aptly repo create --distribution=${DISTRO} ${DISTRO}
		if err = utils.ExecLogOutput(utils.Logger, "aptly", commandTimeout, "repo", "create", "--distribution="+osVersion, osVersion); err != nil {
			return err
		}
		utils.Logger.Printf("[✔] Local repo created for os %s", osVersion)

		// Mirror repo start
		srcRepo := generateAptSrcRepoUrl(upload.SrcRepo, conf.MirrorHost)
		err = mirrorAPTRepo(conf, srcRepo, osVersion)
		if err != nil {
			return err
		}

		for _, arch := range archs {
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
			filePath := path.Join(conf.ArtifactsDestFolder, dest, aptPoolMain, string(fileName[0]), "/", conf.AppName, fileName)

			utils.Logger.Printf("[ ] Add package %s into deb repo for %s/%s", srcPath, osVersion, arch)
			if err = utils.ExecLogOutput(utils.Logger, "aptly", commandTimeout, "repo", "add", "-force-replace=true", osVersion, srcPath); err != nil {
				return err
			}
			utils.Logger.Printf("[✔] Added successfully package into deb repo for %s/%s", osVersion, arch)

			// Create the directory and copy the binary
			if err = utils.ExecWithRetries(s3Retries, utils.S3RemountFn, utils.Logger, "mkdir", commandTimeout, "-p", path.Dir(filePath)); err != nil {
				return err
			}
			if err = utils.ExecWithRetries(s3Retries, utils.S3RemountFn, utils.Logger, "cp", commandTimeout, "-f", srcPath, filePath); err != nil {
				return err
			}
		}

		utils.Logger.Printf("[ ] Publish deb repo for %s", osVersion)
		if err = utils.ExecLogOutput(utils.Logger, "aptly", commandTimeout, "publish", "repo", "-origin=New Relic", "-keyring", conf.GpgKeyRing, "-passphrase", conf.GpgPassphrase, "-batch", osVersion); err != nil {
			return err
		}

		utils.Logger.Printf("[✔] Published successfully deb repo for %s", osVersion)
		if err = syncAPTMetadata(conf, destPath, osVersion); err != nil {
			return err
		}
		utils.Logger.Printf("[✔] Synced successfully local repo for %s into s3", osVersion)
	}

	return nil
}

func syncAPTMetadata(conf config.Config, destPath string, osVersion string) (err error) {
	if _, err = os.Stat(destPath); os.IsNotExist(err) {
		// set right permissions
		err = os.MkdirAll(destPath, 0744)
		if err != nil {
			return err
		}
	}
	utils.Logger.Printf("[ ] Sync local repo for %s into s3", osVersion)
	if err = utils.ExecLogOutput(utils.Logger, "cp", commandTimeout, "-rf", conf.AptlyFolder+"/public/"+aptDists+osVersion, destPath); err != nil {
		return err
	}
	// drop local published repo, to be able to recreate it later
	if err = utils.ExecLogOutput(utils.Logger, "aptly", commandTimeout, "publish", "drop", osVersion); err != nil {
		return err
	}

	mirrorName := "mirror-" + osVersion
	var mirrorExists bool
	//ensure local mirror exists, if not -just created- don't try to drop it
	if err = utils.ExecLogOutput(utils.Logger, "aptly", commandTimeout, "mirror", "show", mirrorName); err == nil {
		mirrorExists = true
	}

	if mirrorExists {
		// drop local mirror, to be able to recreate it later
		if err = utils.ExecLogOutput(utils.Logger, "aptly", commandTimeout, "mirror", "drop", "-force", mirrorName); err != nil {
			return err
		}
	}

	// drop local repo, to be able to recreate it later
	if err = utils.ExecLogOutput(utils.Logger, "aptly", commandTimeout, "repo", "drop", osVersion); err != nil {
		return err
	}

	// rm local repo files, as aptly keep them
	if err = utils.ExecLogOutput(utils.Logger, "rm", commandTimeout, "-rf", conf.AptlyFolder+"/public/"+aptDists+osVersion); err != nil {
		return err
	}
	utils.Logger.Printf("[✔] Sync local repo was successful for %s into s3", osVersion)

	return err
}

func mirrorAPTRepo(conf config.Config, repoUrl string, osVersion string) (err error) {

	// Creating test Url, example http://bucket.fqdn/infrastructure_agent/linux/apt/dists/xenial/Release
	u, err := url.Parse(repoUrl + "/" + aptDists + osVersion + "/Release")
	if err != nil {
		return err
	}

	// Checking if the repo already exists if not mirroring is useless
	resp, err := http.Get(u.String())
	if err != nil {
		return err
	}
	if resp.StatusCode == http.StatusNotFound {
		utils.Logger.Printf("[X] Mirroring skipped since since %s was not present in mounted repo", u.String())
		return nil
	}

	utils.Logger.Printf("[ ] Mirror create APT repo %s for %s", osVersion, repoUrl)
	if err = utils.ExecLogOutput(utils.Logger, "aptly", commandTimeout, "mirror", "create", "-keyring", conf.GpgKeyRing, "mirror-"+osVersion, repoUrl, osVersion, "main"); err != nil {
		return err
	}
	utils.Logger.Printf("[✔] Mirror create succesfully APT repo for %s", osVersion)

	utils.Logger.Printf("[ ] Mirror update APT repo for %s", osVersion)
	if err = utils.ExecLogOutput(utils.Logger, "aptly", commandTimeout, "mirror", "update", "-keyring", conf.GpgKeyRing, "mirror-"+osVersion); err != nil {
		return err
	}
	utils.Logger.Printf("[✔] Mirror update succesfully APT repo for %s", osVersion)

	// The last parameter is `Name` that means a query matches all the packages (as it means “package name is not empty”).
	utils.Logger.Printf("[ ] Mirror repo import APT repo for %s", osVersion)
	if err = utils.ExecLogOutput(utils.Logger, "aptly", commandTimeout, "repo", "import", "mirror-"+osVersion, osVersion, "Name"); err != nil {
		return err
	}
	utils.Logger.Printf("[✔] Mirror repo import succesfully APT repo for %s", osVersion)

	return nil
}

func uploadFileArtifact(conf config.Config, schema config.UploadArtifactSchema, upload config.Upload, arch string) (err error) {
	srcPath, destPath := replaceSrcDestTemplates(
		schema.Src,
		upload.Dest,
		conf.RepoName,
		conf.AppName,
		arch,
		conf.Tag,
		conf.Version,
		conf.DestPrefix,
		"")

	srcPath = path.Join(conf.ArtifactsSrcFolder, srcPath)
	destPath = path.Join(conf.ArtifactsDestFolder, destPath)

	err = utils.CopyFile(srcPath, destPath, upload.Override, commandTimeout)
	if err != nil {
		return err
	}

	return nil
}

func generateDestinationAssetsPath(downloadedFileName, destPathTemplate, repoName, appName, arch, tag, version, destPrefix, osVersion string) string {
	destPath := utils.ReplacePlaceholders(destPathTemplate, repoName, appName, arch, tag, version, destPrefix, osVersion)
	return strings.Replace(destPath, utils.PlaceholderForSrc, downloadedFileName, -1)
}

func generateAptSrcRepoUrl(template, accessPointHost string) (url string) {
	url = strings.Replace(template, utils.PlaceholderForAccessPointHost, accessPointHost, -1)

	return
}

func generateRepoFileContent(accessPointHost, destPath string) (repoFileContent string) {

	contentTemplate := `[newrelic-infra]
name=New Relic Infrastructure
baseurl=%s/%s
gpgkey=https://download.newrelic.com/infrastructure_agent/gpg/newrelic-infra.gpg
gpgcheck=1
repo_gpgcheck=1`

	repoFileContent = fmt.Sprintf(contentTemplate, accessPointHost, destPath)

	return
}

// @TODO: deprecate this function and use generateDestinationAssetsPath() generateDownloadFileName()
func replaceSrcDestTemplates(srcFileTemplate, destPathTemplate, repoName, appName, arch, tag, version, destPrefix, osVersion string) (srcFile string, destPath string) {
	srcFile = utils.ReplacePlaceholders(srcFileTemplate, repoName, appName, arch, tag, version, destPrefix, osVersion)
	destPath = utils.ReplacePlaceholders(destPathTemplate, repoName, appName, arch, tag, version, destPrefix, osVersion)
	destPath = strings.Replace(destPath, utils.PlaceholderForSrc, srcFile, -1)

	return
}
