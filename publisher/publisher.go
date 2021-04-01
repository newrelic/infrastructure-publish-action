// Copyright 2021 New Relic Corporation. All rights reserved.
// SPDX-License-Identifier: Apache-2.0
package main

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/newrelic/infrastructure-publish-action/publisher/lock"
	"github.com/spf13/viper"
	"gopkg.in/yaml.v2"
)

const (
	placeholderForOsVersion  = "{os_version}"
	placeholderForDestPrefix = "{dest_prefix}"
	placeholderForRepoName   = "{repo_name}"
	placeholderForAppName    = "{app_name}"
	placeholderForArch       = "{arch}"
	placeholderForTag        = "{tag}"
	placeholderForVersion    = "{version}"
	placeholderForSrc        = "{src}"
	urlTemplate              = "https://github.com/{repo_name}/releases/download/{tag}/{src}"

	//Errors
	noDestinationError = "no uploads were provided for the schema"

	//FileTypes
	typeFile           = "file"
	typeZypp           = "zypp"
	typeYum            = "yum"
	typeApt            = "apt"
	repodataRpmPath    = "/repodata/repomd.xml"
	signatureRpmPath   = "/repodata/repomd.xml.asc"
	defaultAptlyFolder = "/root/.aptly"
	defaultLockRetries = 10
	defaultLockgroup   = "lockgroup"
	aptPoolMain        = "pool/main/"
	aptDists           = "dists/"
	commandTimeout     = time.Hour * 1
)

var (
	l                = log.New(log.Writer(), "", 0)
	streamExecOutput = true
)

type config struct {
	lockMode             lock.Mode // modes: "disabled", "retry_when_busy" (default), "fail_when_busy"
	destPrefix           string
	repoName             string
	appName              string
	tag                  string
	runID                string
	version              string
	artifactsDestFolder  string
	artifactsSrcFolder   string
	aptlyFolder          string
	uploadSchemaFilePath string
	gpgPassphrase        string
	gpgKeyRing           string
	awsLockBucket        string
	lockGroup            string
	awsRegion            string
	awsRoleARN           string
}

func (c *config) owner() string {
	return fmt.Sprintf("%s_%s_%s", c.appName, c.tag, c.runID)
}

type uploadArtifactSchema struct {
	Src     string   `yaml:"src"`
	Arch    []string `yaml:"arch"`
	Uploads []Upload `yaml:"uploads"`
}

type Upload struct {
	Type      string   `yaml:"type"` // verify type in allowed list file, apt, yum, zypp
	SrcRepo   string   `yaml:"src_repo"`
	Dest      string   `yaml:"dest"`
	Override  bool     `yaml:"override"`
	OsVersion []string `yaml:"os_version"`
}

type uploadArtifactsSchema []uploadArtifactSchema

func main() {
	conf := loadConfig()

	if !conf.lockMode.IsValid() {
		l.Fatal("invalid lock mode")
	}

	var bucketLock lock.BucketLock
	if conf.lockMode.IsDisabled() {
		bucketLock = lock.NewNoop()
	} else {
		if conf.awsRegion == "" {
			l.Fatal("missing 'aws_region' value")
		}
		if conf.awsLockBucket == "" {
			l.Fatal("missing 'aws_s3_lock_bucket_name' value")
		}
		if conf.awsRoleARN == "" {
			l.Fatal("missing 'aws_role_arn' value")
		}
		if conf.runID == "" {
			l.Fatal("missing 'run_id' value")
		}

		// TODO parametrise
		// resource tags
		var (
			tagOwningTeam = "CAOS"
			tagProduct    = "integrations"
			tagProject    = "infrastructure-publish-action"
			tagEnv        = "us-development"
		)
		tags := fmt.Sprintf("department=product&product=%s&project=%s&owning_team=%s&environment=%s", tagProduct, tagProject, tagOwningTeam, tagEnv)

		var maxRetries uint
		if conf.lockMode.IsRetryOnBusy() {
			maxRetries = defaultLockRetries
		}
		cfg := lock.NewS3Config(
			conf.awsLockBucket,
			conf.awsLockBucket,
			conf.awsRegion,
			tags,
			conf.lockGroup,
			conf.owner(),
			maxRetries,
			lock.DefaultRetryBackoff,
			lock.DefaultTTL,
		)
		var err error
		bucketLock, err = lock.NewS3(cfg, l.Printf)
		// fail fast when lacking required AWS credentials
		if err != nil {
			l.Fatal("cannot create lock on s3: " + err.Error())
		}
	}

	uploadSchemaContent, err := readFileContent(conf.uploadSchemaFilePath)
	if err != nil {
		l.Fatal(err)
	}

	uploadSchema, err := parseUploadSchema(uploadSchemaContent)
	if err != nil {
		l.Fatal(err)
	}

	d := newDownloader(http.DefaultClient)
	err = d.downloadArtifacts(conf, uploadSchema)
	if err != nil {
		l.Fatal(err)
	}
	l.Println("🎉 download phase complete")

	err = uploadArtifacts(conf, uploadSchema, bucketLock)
	if err != nil {
		l.Fatal(err)
	}
	l.Println("🎉 upload phase complete")
}

func loadConfig() config {
	// TODO: make all the config required
	viper.BindEnv("repo_name")
	viper.BindEnv("app_name")
	viper.BindEnv("tag")
	viper.BindEnv("run_id")
	viper.BindEnv("artifacts_dest_folder")
	viper.BindEnv("artifacts_src_folder")
	viper.BindEnv("aptly_folder")
	viper.BindEnv("upload_schema_file_path")
	viper.BindEnv("dest_prefix")
	viper.BindEnv("gpg_passphrase")
	viper.BindEnv("gpg_key_ring")
	viper.BindEnv("aws_s3_bucket_name")
	viper.BindEnv("aws_s3_lock_bucket_name")
	viper.BindEnv("aws_role_arn")
	viper.BindEnv("aws_region")
	viper.BindEnv("lock")

	aptlyF := viper.GetString("aptly_folder")
	if aptlyF == "" {
		aptlyF = defaultAptlyFolder
	}

	lockGroup := viper.GetString("lock_group")
	if lockGroup == "" {
		lockGroup = defaultLockgroup
	}

	return config{
		destPrefix:           viper.GetString("dest_prefix"),
		repoName:             viper.GetString("repo_name"),
		appName:              viper.GetString("app_name"),
		tag:                  viper.GetString("tag"),
		runID:                viper.GetString("run_id"),
		version:              strings.Replace(viper.GetString("tag"), "v", "", -1),
		artifactsDestFolder:  viper.GetString("artifacts_dest_folder"),
		artifactsSrcFolder:   viper.GetString("artifacts_src_folder"),
		aptlyFolder:          aptlyF,
		uploadSchemaFilePath: viper.GetString("upload_schema_file_path"),
		gpgPassphrase:        viper.GetString("gpg_passphrase"),
		gpgKeyRing:           viper.GetString("gpg_key_ring"),
		lockGroup:            lockGroup,
		awsLockBucket:        viper.GetString("aws_s3_lock_bucket_name"),
		awsRoleARN:           viper.GetString("aws_role_arn"),
		awsRegion:            viper.GetString("aws_region"),
		lockMode:             lock.Mode(viper.GetString("lock")),
	}
}

func readFileContent(filePath string) ([]byte, error) {
	fileContent, err := ioutil.ReadFile(filePath)

	return fileContent, err
}

func parseUploadSchema(fileContent []byte) (uploadArtifactsSchema, error) {

	var schema uploadArtifactsSchema

	err := yaml.Unmarshal(fileContent, &schema)

	if err != nil {
		return nil, err
	}

	for i := range schema {
		if schema[i].Arch == nil {
			schema[i].Arch = []string{""}
		}
		if len(schema[i].Uploads) == 0 {
			return nil, fmt.Errorf("error: '%s' in the schema: %v ", noDestinationError, schema[i].Src)
		}
	}

	return schema, nil
}

func (d *downloader) downloadArtifact(conf config, src, arch, osVersion string) error {

	l.Println("Starting downloading artifacts!")

	srcFile := replacePlaceholders(src, conf.repoName, conf.appName, arch, conf.tag, conf.version, conf.destPrefix, osVersion)
	url := generateDownloadUrl(urlTemplate, conf.repoName, conf.tag, srcFile)

	destPath := path.Join(conf.artifactsSrcFolder, srcFile)

	l.Println(fmt.Sprintf("[ ] Download %s into %s", url, destPath))

	err := d.downloadFile(url, destPath)
	if err != nil {
		return err
	}

	fi, err := os.Stat(destPath)
	if err != nil {
		return err
	}

	l.Println(fmt.Sprintf("[✔] Download %s into %s %d bytes", url, destPath, fi.Size()))

	return nil
}

func (d *downloader) downloadFile(url, destPath string) error {
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return err
	}

	response, err := d.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer response.Body.Close()

	if response.StatusCode != 200 {
		return fmt.Errorf("error on download %s with status code %v", url, response.StatusCode)
	}

	file, err := os.Create(destPath)
	if err != nil {
		return err
	}
	defer file.Close()

	_, err = io.Copy(file, response.Body)
	if err != nil {
		return err
	}

	return nil
}

type HTTPClient interface {
	Do(req *http.Request) (*http.Response, error)
}

type downloader struct {
	httpClient HTTPClient
}

func newDownloader(client HTTPClient) *downloader {
	return &downloader{
		httpClient: client,
	}
}

func (d *downloader) downloadArtifacts(conf config, schema uploadArtifactsSchema) error {
	for _, artifactSchema := range schema {
		var osVersions []string
		for _, up := range artifactSchema.Uploads {
			osVersions = append(osVersions, up.OsVersion...)
		}

		if len(osVersions) == 0 {
			osVersions = []string{""}
		}

		for _, osVersion := range osVersions {
			for _, arch := range artifactSchema.Arch {
				err := d.downloadArtifact(conf, artifactSchema.Src, arch, osVersion)
				if err != nil {
					return err
				}
			}
		}
	}
	return nil
}

func uploadArtifact(conf config, schema uploadArtifactSchema, arch string, upload Upload) (err error) {

	if upload.Type == typeFile {
		l.Println("Uploading file artifact")
		err = uploadFileArtifact(conf, schema, upload, arch)
	} else if upload.Type == typeYum || upload.Type == typeZypp {
		l.Println("Uploading rpm as yum or zypp")
		err = uploadRpm(conf, schema.Src, upload, arch)
	} else if upload.Type == typeApt {
		l.Println("Uploading apt")
		err = uploadApt(conf, schema.Src, upload, arch)
	}
	if err != nil {
		return err
	}

	return nil
}

func uploadArtifacts(conf config, schema uploadArtifactsSchema, bucketLock lock.BucketLock) (err error) {
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
		for _, arch := range artifactSchema.Arch {
			for _, upload := range artifactSchema.Uploads {
				err := uploadArtifact(conf, artifactSchema, arch, upload)
				if err != nil {
					return err
				}
			}
		}
	}
	return nil
}

func uploadRpm(conf config, srcTemplate string, upload Upload, arch string) (err error) {

	for _, osVersion := range upload.OsVersion {
		l.Printf("[ ] Start uploading rpm for os %s/%s", osVersion, arch)

		fileName, destPath := replaceSrcDestTemplates(
			srcTemplate,
			upload.Dest,
			conf.repoName,
			conf.appName,
			arch,
			conf.tag,
			conf.version,
			conf.destPrefix,
			osVersion)

		srcPath := path.Join(conf.artifactsSrcFolder, fileName)
		repoPath := path.Join(conf.artifactsDestFolder, destPath)
		filePath := path.Join(repoPath, fileName)
		repomd := path.Join(repoPath, repodataRpmPath)
		signaturePath := path.Join(repoPath, signatureRpmPath)

		err = copyFile(srcPath, filePath, upload.Override)
		if err != nil {
			return err
		}

		if _, err = os.Stat(repomd); os.IsNotExist(err) {

			l.Printf("[ ] Didn't fine repo for %s, run repo init command", repoPath)

			if err := execLogOutput(l, "createrepo", repoPath, "-o", os.TempDir()); err != nil {
				return err
			}

			l.Printf("[✔] Repo created: %s", repoPath)
		} else {
			_ = os.Remove(signaturePath)
		}

		// "cache" the repodata so it doesnt have to process all again
		if err = execLogOutput(l, "cp", "-rf", repoPath+"/repodata/", os.TempDir()+"/repodata/"); err != nil {
			return err
		}

		if err = execLogOutput(l, "createrepo", "--update", "-s", "sha", repoPath, "-o", os.TempDir()); err != nil {
			return err
		}

		// remove the 'old' repodata
		if err = execLogOutput(l, "rm", "-rf", repoPath+"/repodata/"); err != nil {
			return err
		}

		// copy from temp repodata to repo repodata
		if err = execLogOutput(l, "cp", "-rf", os.TempDir()+"/repodata/", repoPath); err != nil {
			return err
		}

		// remove temp repodata so the next repo doesn't get confused
		if err = execLogOutput(l, "rm", "-rf", os.TempDir()+"/repodata/"); err != nil {
			return err
		}

		_, err = os.Stat(repomd)

		if err != nil {
			return fmt.Errorf("error while creating repository %s for source %s and destination %s", err.Error(), srcPath, destPath)
		}

		if err := execLogOutput(l, "gpg", "--batch", "--pinentry-mode=loopback", "--passphrase", conf.gpgPassphrase, "--keyring", conf.gpgKeyRing, "--detach-sign", "--armor", repomd); err != nil {
			return err
		}
		l.Printf("[✔] Uploading RPM succeded for src %s and dest %s \n", srcPath, destPath)
	}

	return nil
}

func uploadApt(conf config, srcTemplate string, upload Upload, arch string) (err error) {

	// the dest path for apt is the same for each distribution since it does not depend on it
	var destPath string
	for _, osVersion := range upload.OsVersion {
		l.Printf("[ ] Start uploading deb for os %s/%s", osVersion, arch)

		fileName, dest := replaceSrcDestTemplates(
			srcTemplate,
			upload.Dest,
			conf.repoName,
			conf.appName,
			arch,
			conf.tag,
			conf.version,
			conf.destPrefix,
			osVersion)

		srcPath := path.Join(conf.artifactsSrcFolder, fileName)
		destPath = path.Join(conf.artifactsDestFolder, dest, aptDists)
		filePath := path.Join(conf.artifactsDestFolder, dest, aptPoolMain, string(fileName[0]), "/", conf.appName, fileName)

		l.Printf("[ ] Create local repo for os %s/%s", osVersion, arch)

		// aptly repo create --distribution=${DISTRO} ${DISTRO}
		if err = execLogOutput(l, "aptly", "repo", "create", "--distribution="+osVersion, osVersion); err != nil {
			return err
		}
		l.Printf("[✔] Local repo created for os %s/%s", osVersion, arch)

		// Mirror repo start
		err = mirrorAPTRepo(conf, upload.SrcRepo, srcPath, osVersion, arch)
		if err != nil {
			return err
		}

		l.Printf("[ ] Add package %s into deb repo for %s/%s", srcPath, osVersion, arch)
		if err = execLogOutput(l, "aptly", "repo", "add", "-force-replace=true", osVersion, srcPath); err != nil {
			return err
		}
		l.Printf("[✔] Added successfully package into deb repo for %s/%s", osVersion, arch)

		l.Printf("[ ] Publish deb repo for %s/%s", osVersion, arch)
		if err = execLogOutput(l, "aptly", "publish", "repo", "-origin=New Relic", "-keyring", conf.gpgKeyRing, "-passphrase", conf.gpgPassphrase, "-batch", osVersion); err != nil {
			return err
		}
		l.Printf("[✔] Published succesfully deb repo for %s/%s", osVersion, arch)

		// Copying the binary
		//if err = copyFile(srcPath, filePath); err != nil {
		//	return err
		//}
		// copy from temp repodata to repo repodata
		if err = execLogOutput(l, "cp", "-f", srcPath, filePath); err != nil {
			return err
		}

		if err = syncAPTMetadata(conf, destPath, osVersion, arch); err != nil {
			return err
		}
	}

	l.Printf("[✔] Synced successfully local repo for %s into s3", arch)
	return nil
}

func syncAPTMetadata(conf config, destPath string, osVersion string, arch string) (err error) {
	if _, err = os.Stat(destPath); os.IsNotExist(err) {
		// set right permissions
		err = os.MkdirAll(destPath, 0744)
		if err != nil {
			return err
		}
	}
	l.Printf("[ ] Sync local repo for %s/%s into s3", osVersion, arch)
	if err = execLogOutput(l, "cp", "-rf", conf.aptlyFolder+"/public/"+aptDists+osVersion, destPath); err != nil {
		return err
	}
	l.Printf("[✔] Sync local repo was successful for %s/%s into s3", osVersion, arch)

	return err
}

func mirrorAPTRepo(conf config, repoUrl string, srcPath string, osVersion string, arch string) (err error) {

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
		l.Printf("[X] Mirroring skipped since since %s was not present in mounted repo", u.String())
		return nil
	}

	l.Printf("[ ] Mirror create APT repo for %s/%s/%s from %s", srcPath, osVersion, arch, repoUrl)
	if err = execLogOutput(l, "aptly", "mirror", "create", "-keyring", conf.gpgKeyRing, "mirror-"+osVersion, repoUrl, osVersion, "main"); err != nil {
		return err
	}
	l.Printf("[✔] Mirror create succesfully APT repo for %s/%s/%s", srcPath, osVersion, arch)

	l.Printf("[ ] Mirror update APT repo for %s/%s/%s", srcPath, osVersion, arch)
	if err = execLogOutput(l, "aptly", "mirror", "update", "-keyring", conf.gpgKeyRing, "mirror-"+osVersion); err != nil {
		return err
	}
	l.Printf("[✔] Mirror update succesfully APT repo for %s/%s/%s", srcPath, osVersion, arch)

	// The last parameter is `Name` that means a query matches all the packages (as it means “package name is not empty”).
	l.Printf("[ ] Mirror repo import APT repo for %s/%s/%s", srcPath, osVersion, arch)
	if err = execLogOutput(l, "aptly", "repo", "import", "mirror-"+osVersion, osVersion, "Name"); err != nil {
		return err
	}
	l.Printf("[✔] Mirror repo import succesfully APT repo for %s/%s/%s", srcPath, osVersion, arch)

	return nil
}

// execLogOutput executes a command writing stdout & stderr to provided l.
func execLogOutput(l *log.Logger, cmdName string, cmdArgs ...string) (err error) {
	ctx, cancel := context.WithTimeout(context.Background(), commandTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, cmdName, cmdArgs...)

	l.Printf("Executing in shell '%s'", cmd.String())

	if !streamExecOutput {
		output, err := cmd.CombinedOutput()
		l.Println(string(output))
		return err
	}

	stdoutR, err := cmd.StdoutPipe()
	if err != nil {
		return
	}
	stderrR, err := cmd.StderrPipe()
	if err != nil {
		return
	}

	wg := sync.WaitGroup{}
	wg.Add(2)
	defer l.Println()
	if err = cmd.Start(); err != nil {
		return err
	}

	go streamAsLog(&wg, l, stdoutR, "stdout")
	go streamAsLog(&wg, l, stderrR, "stderr")

	wg.Wait()
	return cmd.Wait()
}

func streamAsLog(wg *sync.WaitGroup, l *log.Logger, r io.ReadCloser, prefix string) {
	defer wg.Done()

	if prefix != "" {
		prefix += ": "
	}

	stdoutBufR := bufio.NewReader(r)
	var err error
	var line []byte
	for {
		line, _, err = stdoutBufR.ReadLine()
		if err != nil {
			if err == io.EOF {
				return
			}
			l.Fatalf("Got unknown error: %s", err)
		}

		l.Printf("%s%s\n", prefix, string(line))
	}
}

func uploadFileArtifact(conf config, schema uploadArtifactSchema, upload Upload, arch string) (err error) {
	srcPath, destPath := replaceSrcDestTemplates(
		schema.Src,
		upload.Dest,
		conf.repoName,
		conf.appName,
		arch,
		conf.tag,
		conf.version,
		conf.destPrefix,
		"")

	srcPath = path.Join(conf.artifactsSrcFolder, srcPath)
	destPath = path.Join(conf.artifactsDestFolder, destPath)

	err = copyFile(srcPath, destPath, upload.Override)
	if err != nil {
		return err
	}

	return nil
}

func copyFile(srcPath string, destPath string, override bool) (err error) {

	// We do not want to override already pushed packages
	if _, err = os.Stat(destPath); !override && err == nil {
		l.Println(fmt.Sprintf("Skipping copying file '%s': already exists at:  %s", srcPath, destPath))
		return
	}

	destDirectory := filepath.Dir(destPath)

	if _, err = os.Stat(destDirectory); os.IsNotExist(err) {
		// set right permissions
		err = os.MkdirAll(destDirectory, 0744)
		if err != nil {
			return err
		}
	}

	l.Println("[ ] Copy " + srcPath + " into " + destPath)
	input, err := ioutil.ReadFile(srcPath)
	if err != nil {
		return err
	}

	err = ioutil.WriteFile(destPath, input, 0744)
	if err != nil {
		return err
	}

	l.Println("[✔] Copy " + srcPath + " into " + destPath)
	return nil
}

func replacePlaceholders(template, repoName, appName, arch, tag, version, destPrefix, osVersion string) (str string) {
	str = strings.Replace(template, placeholderForRepoName, repoName, -1)
	str = strings.Replace(str, placeholderForAppName, appName, -1)
	str = strings.Replace(str, placeholderForArch, arch, -1)
	str = strings.Replace(str, placeholderForTag, tag, -1)
	str = strings.Replace(str, placeholderForVersion, version, -1)
	str = strings.Replace(str, placeholderForDestPrefix, destPrefix, -1)
	str = strings.Replace(str, placeholderForOsVersion, osVersion, -1)

	return
}

func replaceSrcDestTemplates(srcFileTemplate, destPathTemplate, repoName, appName, arch, tag, version, destPrefix, osVersion string) (srcFile string, destPath string) {
	srcFile = replacePlaceholders(srcFileTemplate, repoName, appName, arch, tag, version, destPrefix, osVersion)
	destPath = replacePlaceholders(destPathTemplate, repoName, appName, arch, tag, version, destPrefix, osVersion)
	destPath = strings.Replace(destPath, placeholderForSrc, srcFile, -1)

	return
}

func generateDownloadUrl(template, repoName, tag, srcFile string) (url string) {
	url = replacePlaceholders(template, repoName, "", "", tag, "", "", "")
	url = strings.Replace(url, placeholderForSrc, srcFile, -1)

	return
}
