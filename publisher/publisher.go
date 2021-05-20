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
	placeholderForOsVersion       = "{os_version}"
	placeholderForDestPrefix      = "{dest_prefix}"
	placeholderForRepoName        = "{repo_name}"
	placeholderForAppName         = "{app_name}"
	placeholderForArch            = "{arch}"
	placeholderForTag             = "{tag}"
	placeholderForVersion         = "{version}"
	placeholderForSrc             = "{src}"
	placeholderForAccessPointHost = "{access_point_host}"
	urlTemplate                   = "https://github.com/{repo_name}/releases/download/{tag}/{src}"

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
	defaultLockRetries = 30
	defaultLockgroup   = "lockgroup"
	aptPoolMain        = "pool/main/"
	aptDists           = "dists/"
	commandTimeout     = time.Hour * 1

	// AWS lock resource tags
	defaultTagOwningTeam = "CAOS"
	defaultTagProduct    = "integrations"
	defaultTagProject    = "infrastructure-publish-action"
	defaultTagEnv        = "us-development"

	//Access points
	accessPointStaging               = "https://nr-downloads-ohai-staging.s3-website-us-east-1.amazonaws.com"
	accessPointTesting               = "https://nr-downloads-ohai-testing.s3-website-us-east-1.amazonaws.com"
	accessPointProduction            = "https://download.newrelic.com"
	placeholderAccessPointStaging    = "staging"
	placeholderAccessPointTesting    = "testing"
	placeholderAccessPointProduction = "production"
)

var (
	defaultTags = fmt.Sprintf("department=product&product=%s&project=%s&owning_team=%s&environment=%s",
		defaultTagProduct,
		defaultTagProject,
		defaultTagOwningTeam,
		defaultTagEnv,
	)
)

var (
	l                = log.New(log.Writer(), "", 0)
	streamExecOutput = true
)

type config struct {
	destPrefix           string
	repoName             string
	appName              string
	tag                  string
	accessPointHost      string
	runID                string
	version              string
	artifactsDestFolder  string // s3 mounted folder
	artifactsSrcFolder   string
	aptlyFolder          string
	uploadSchemaFilePath string
	gpgPassphrase        string
	gpgKeyRing           string
	awsRegion            string
	awsRoleARN           string
	// locking properties (candidate for factoring)
	awsLockBucket     string
	awsTags           string
	lockGroup         string
	disableLock       bool
	lockRetries       uint
	useDefLockRetries bool
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

	var bucketLock lock.BucketLock
	if conf.disableLock {
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

		if conf.awsTags == "" {
			conf.awsTags = defaultTags
		}

		if conf.useDefLockRetries {
			conf.lockRetries = defaultLockRetries
		}
		cfg := lock.NewS3Config(
			conf.awsLockBucket,
			conf.awsRoleARN,
			conf.awsRegion,
			conf.awsTags,
			conf.lockGroup,
			conf.owner(),
			conf.lockRetries,
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
	viper.BindEnv("access_point_host")
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
	viper.BindEnv("disable_lock")
	viper.BindEnv("lock_retries")

	aptlyF := viper.GetString("aptly_folder")
	if aptlyF == "" {
		aptlyF = defaultAptlyFolder
	}

	lockGroup := viper.GetString("lock_group")
	if lockGroup == "" {
		lockGroup = defaultLockgroup
	}

	accessPointHost := parseAccessPointHost(viper.GetString("access_point_host"))

	return config{
		destPrefix:           viper.GetString("dest_prefix"),
		repoName:             viper.GetString("repo_name"),
		appName:              viper.GetString("app_name"),
		tag:                  viper.GetString("tag"),
		accessPointHost:      accessPointHost,
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
		awsTags:              viper.GetString("aws_tags"),
		disableLock:          viper.GetBool("disable_lock"),
		lockRetries:          viper.GetUint("lock_retries"),
		useDefLockRetries:    !viper.IsSet("lock_retries"), // when non set: use default value
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

	// RPM specific architecture variables
	destPathArch := arch
	switch destPathArch {
	case "arm64":
		destPathArch = "aarch64"
	}

	for _, osVersion := range upload.OsVersion {
		l.Printf("[ ] Start uploading rpm for os %s/%s", osVersion, arch)

		downloadedRpmFileName := generateDownloadFileName(
			srcTemplate,
			conf.repoName,
			conf.appName,
			arch,
			conf.tag,
			conf.version,
			conf.destPrefix,
			osVersion)

		destPath := generateDestinationAssetsPath(
			downloadedRpmFileName,
			upload.Dest,
			conf.repoName,
			conf.appName,
			destPathArch,
			conf.tag,
			conf.version,
			conf.destPrefix,
			osVersion)

		downloadedRpmFilePath := path.Join(conf.artifactsSrcFolder, downloadedRpmFileName)
		s3RepoPath := path.Join(conf.artifactsDestFolder, destPath)
		s3DotRepoFilepath := path.Join(s3RepoPath, "newrelic-infra.repo")
		s3RepoData := path.Join(s3RepoPath, "repodata")
		rpmDestinationPath := path.Join(s3RepoPath, downloadedRpmFileName)
		s3RepomdFilepath := path.Join(s3RepoPath, repodataRpmPath)
		signaturePath := path.Join(s3RepoPath, signatureRpmPath)

		// copy rpm file to be able to add it into the index later
		err = copyFile(downloadedRpmFilePath, rpmDestinationPath, upload.Override)
		if err != nil {
			return err
		}

		// check for repo and create if missing
		if _, err = os.Stat(s3RepomdFilepath); os.IsNotExist(err) {

			l.Printf("[ ] Didn't find repo for %s, run repo init command", s3RepoPath)

			if err := execLogOutput(l, "createrepo", s3RepoPath, "-o", os.TempDir()); err != nil {
				return err
			}

			l.Printf("[✔] Repo created: %s", s3RepoPath)
		} else {
			_ = os.Remove(signaturePath)
		}

		// check for .repo file and create if needed
		if _, err = os.Stat(s3DotRepoFilepath); conf.accessPointHost != "" && os.IsNotExist(err) {
			l.Println(fmt.Sprintf("creating 'newrelic-infra.repo' file in %s", s3RepoPath))

			repoFileContent := generateRepoFileContent(conf.accessPointHost, destPath)

			err := ioutil.WriteFile(s3DotRepoFilepath, []byte(repoFileContent), 0644)
			if err != nil {
				return err
			}
		}

		// "cache" the repodata from s3 to local so it doesnt have to process all again
		if _, err = os.Stat(s3RepoData + "/"); err == nil {
			if err = execLogOutput(l, "cp", "-rf", s3RepoData+"/", os.TempDir()+"/repodata/"); err != nil {
				return err
			}
		}

		if err = execLogOutput(l, "createrepo", "--update", "-s", "sha", s3RepoPath, "-o", os.TempDir()); err != nil {
			return err
		}

		// remove the 'old' repodata from s3
		if err = execLogOutput(l, "rm", "-rf", s3RepoData+"/"); err != nil {
			return err
		}

		// copy from temp repodata to repo repodata in s3
		if err = execLogOutput(l, "cp", "-rf", os.TempDir()+"/repodata/", s3RepoPath); err != nil {
			return err
		}

		// remove temp repodata so the next repo doesn't get confused
		if err = execLogOutput(l, "rm", "-rf", os.TempDir()+"/repodata/"); err != nil {
			return err
		}

		// verify that metadata copied to s3
		if _, err = os.Stat(s3RepomdFilepath); err != nil {
			return fmt.Errorf("error while creating repository %s for source %s and destination %s", err.Error(), downloadedRpmFilePath, destPath)
		}

		// sign metadata with GPG key
		if err := execLogOutput(l, "gpg", "--batch", "--pinentry-mode=loopback", "--passphrase", conf.gpgPassphrase, "--keyring", conf.gpgKeyRing, "--detach-sign", "--armor", s3RepomdFilepath); err != nil {
			return err
		}

		l.Printf("[✔] Uploading RPM succeded for src %s and dest %s \n", downloadedRpmFilePath, destPath)
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
		srcRepo := generateAptSrcRepoUrl(upload.SrcRepo, conf.accessPointHost)
		err = mirrorAPTRepo(conf, srcRepo, srcPath, osVersion, arch)
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

		// Create the directory and copy the binary
		if err = execLogOutput(l, "mkdir", "-p", path.Dir(filePath)); err != nil {
			return err
		}
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
	// drop local published repo, to be able to recreate it later
	if err = execLogOutput(l, "aptly", "publish", "drop", osVersion); err != nil {
		return err
	}
	// drop local mirror, to be able to recreate it later
	if err = execLogOutput(l, "aptly", "mirror", "drop", "-force", "mirror-"+osVersion); err != nil {
		return err
	}
	// drop local repo, to be able to recreate it later
	if err = execLogOutput(l, "aptly", "repo", "drop", osVersion); err != nil {
		return err
	}
	// rm local repo files, as aptly keep them
	if err = execLogOutput(l, "rm", "-rf", conf.aptlyFolder+"/public/"+aptDists+osVersion); err != nil {
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

	l.Printf("[ ] Mirror create APT repo %s for %s/%s from %s", srcPath, osVersion, arch, repoUrl)
	if err = execLogOutput(l, "aptly", "mirror", "create", "-keyring", conf.gpgKeyRing, "mirror-"+osVersion, repoUrl, osVersion, "main"); err != nil {
		return err
	}
	l.Printf("[✔] Mirror create succesfully APT repo %s for %s/%s", srcPath, osVersion, arch)

	l.Printf("[ ] Mirror update APT repo %s for %s/%s", srcPath, osVersion, arch)
	if err = execLogOutput(l, "aptly", "mirror", "update", "-keyring", conf.gpgKeyRing, "mirror-"+osVersion); err != nil {
		return err
	}
	l.Printf("[✔] Mirror update succesfully APT repo %s for %s/%s", srcPath, osVersion, arch)

	// The last parameter is `Name` that means a query matches all the packages (as it means “package name is not empty”).
	l.Printf("[ ] Mirror repo import APT repo %s for %s/%s", srcPath, osVersion, arch)
	if err = execLogOutput(l, "aptly", "repo", "import", "mirror-"+osVersion, osVersion, "Name"); err != nil {
		return err
	}
	l.Printf("[✔] Mirror repo import succesfully APT repo %s for %s/%s", srcPath, osVersion, arch)

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

// @TODO: deprecate this function and use generateDestinationAssetsPath() generateDownloadFileName()
func replaceSrcDestTemplates(srcFileTemplate, destPathTemplate, repoName, appName, arch, tag, version, destPrefix, osVersion string) (srcFile string, destPath string) {
	srcFile = replacePlaceholders(srcFileTemplate, repoName, appName, arch, tag, version, destPrefix, osVersion)
	destPath = replacePlaceholders(destPathTemplate, repoName, appName, arch, tag, version, destPrefix, osVersion)
	destPath = strings.Replace(destPath, placeholderForSrc, srcFile, -1)

	return
}

func generateDestinationAssetsPath(downloadedFileName, destPathTemplate, repoName, appName, arch, tag, version, destPrefix, osVersion string) string {
	destPath := replacePlaceholders(destPathTemplate, repoName, appName, arch, tag, version, destPrefix, osVersion)
	return strings.Replace(destPath, placeholderForSrc, downloadedFileName, -1)
}

func generateDownloadFileName(srcFileTemplate, repoName, appName, arch, tag, version, destPrefix, osVersion string) string {
	return replacePlaceholders(srcFileTemplate, repoName, appName, arch, tag, version, destPrefix, osVersion)
}

func generateDownloadUrl(template, repoName, tag, srcFile string) (url string) {
	url = replacePlaceholders(template, repoName, "", "", tag, "", "", "")
	url = strings.Replace(url, placeholderForSrc, srcFile, -1)

	return
}

func generateAptSrcRepoUrl(template, accessPointHost string) (url string) {
	url = strings.Replace(template, placeholderForAccessPointHost, accessPointHost, -1)

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

// parseAccessPointHost accessPointHost will be parsed to detect production, staging or testing placeholders
// and substitute them with their specific real values. Empty will fallback to production and any other value
// will be considered a different access point and will be return as it is
func parseAccessPointHost(accessPointHost string) string {
	switch accessPointHost {
	case "":
		return accessPointProduction
	case placeholderAccessPointProduction:
		return accessPointProduction
	case placeholderAccessPointStaging:
		return accessPointStaging
	case placeholderAccessPointTesting:
		return accessPointTesting
	default:
		return accessPointHost
	}
}