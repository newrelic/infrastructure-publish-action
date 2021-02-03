package main

import (
	"fmt"
	"github.com/spf13/viper"
	"gopkg.in/yaml.v2"
	"io"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strings"
	"time"
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
	typeFile        = "file"
	typeZypp        = "zypp"
	typeYum         = "yum"
	typeApt         = "apt"
	repodataRpmPath = "/repodata/repomd.xml"

	timeoutFileCreation = time.Second * 300
)

type config struct {
	destPrefix           string
	repoName             string
	appName              string
	tag                  string
	version              string
	artifactsDestFolder  string
	artifactsSrcFolder   string
	uploadSchemaFilePath string
	gpgPassphrase        string
}

type uploadArtifactSchema struct {
	Src     string   `yaml:"src"`
	Arch    []string `yaml:"arch"`
	Uploads []Upload `yaml:"uploads"`
}

type Upload struct {
	Type      string   `yaml:"type"` // verify type in allowed list file, apt, yum, zypp
	SrcRepo   string   `yaml:"source_repo"`
	Dest      string   `yaml:"dest"`
	OsVersion []string `yaml:"os_version"`
}

type uploadArtifactsSchema []uploadArtifactSchema

func main() {
	conf := loadConfig()
	log.Println(fmt.Sprintf("config: %v", conf))

	uploadSchemaContent, err := readFileContent(conf.uploadSchemaFilePath)
	if err != nil {
		log.Fatal(err)
	}

	uploadSchema, err := parseUploadSchema(uploadSchemaContent)
	if err != nil {
		log.Fatal(err)
	}

	err = downloadArtifacts(conf, uploadSchema)

	if err != nil {
		log.Fatal(err)
	}

	log.Println("🎉 download phase complete")

	err = uploadArtifacts(conf, uploadSchema)

	if err != nil {
		log.Fatal(err)
	}

	log.Println("🎉 upload phase complete")
}

func loadConfig() config {
	// TODO: make all the config required
	viper.BindEnv("repo_name")
	viper.BindEnv("app_name")
	viper.BindEnv("tag")
	viper.BindEnv("artifacts_dest_folder")
	viper.BindEnv("artifacts_src_folder")
	viper.BindEnv("upload_schema_file_path")
	viper.BindEnv("dest_prefix")
	viper.BindEnv("gpg_passphrase")

	return config{
		destPrefix:           viper.GetString("dest_prefix"),
		repoName:             viper.GetString("repo_name"),
		appName:              viper.GetString("app_name"),
		tag:                  viper.GetString("tag"),
		version:              strings.Replace(viper.GetString("tag"), "v", "", -1),
		artifactsDestFolder:  viper.GetString("artifacts_dest_folder"),
		artifactsSrcFolder:   viper.GetString("artifacts_src_folder"),
		uploadSchemaFilePath: viper.GetString("upload_schema_file_path"),
		gpgPassphrase:        viper.GetString("gpg_passphrase"),
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

	for i, _ := range schema {
		if schema[i].Arch == nil {
			schema[i].Arch = []string{""}
		}
		if len(schema[i].Uploads) == 0 {
			return nil, fmt.Errorf("error: '%s' in the schema: %v ", noDestinationError, schema[i].Src)
		}
	}

	return schema, nil
}

func downloadArtifact(conf config, schema uploadArtifactSchema) error {

	log.Println("Starting downloading artifacts!")
	for _, arch := range schema.Arch {
		srcFile := replacePlaceholders(schema.Src, conf.repoName, conf.appName, arch, conf.tag, conf.version, conf.destPrefix, "")
		url := generateDownloadUrl(urlTemplate, conf.repoName, conf.tag, srcFile)

		destPath := path.Join(conf.artifactsSrcFolder, srcFile)

		log.Println(fmt.Sprintf("[ ] Download %s into %s", url, destPath))

		err := downloadFile(url, destPath)
		if err != nil {
			return err
		}

		fi, err := os.Stat(destPath)
		if err != nil {
			return err
		}

		log.Println(fmt.Sprintf("[✔] Download %s into %s %d bytes", url, destPath, fi.Size()))
	}

	return nil
}

func downloadArtifacts(conf config, schema uploadArtifactsSchema) error {
	for _, artifactSchema := range schema {
		err := downloadArtifact(conf, artifactSchema)
		if err != nil {
			return err
		}
	}
	return nil
}

func uploadArtifact(conf config, schema uploadArtifactSchema) (err error) {

	for _, arch := range schema.Arch {
		for _, upload := range schema.Uploads {

			if upload.Type == typeFile {
				log.Println("Uploading file artifact")
				err = uploadFileArtifact(conf, schema, upload, arch)
			} else if upload.Type == typeYum || upload.Type == typeZypp {
				log.Println("Uploading rpm as yum or zypp")
				err = uploadRpm(conf, schema.Src, upload, arch)
			} else if upload.Type == typeApt {
				log.Println("Uploading apt")
				err = uploadApt(conf, schema, upload, arch)
			}
			if err != nil {
				return err
			}
		}
	}

	return nil
}

func uploadRpm(conf config, srcTemplate string, upload Upload, arch string) (err error) {

	for _, osVersion := range upload.OsVersion {
		log.Printf("[ ] Start uploading rpm for os %s/%s", osVersion, arch)

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

		err = copyFile(srcPath, filePath)
		if err != nil {
			return err
		}

		if _, err = os.Stat(repomd); os.IsNotExist(err) {

			log.Printf("[ ] Didn't fine repo for %s, run repo init command", repoPath)

			// TODO: set right permissions

			if err := exeCmd("createrepo", repoPath); err != nil {
				return err
			}

			log.Printf("[✔] Repo created: %s", repoPath)
		}

		if err := exeCmd("createrepo", "--update", "-s", "sha", repoPath); err != nil {
			return err
		}

		log.Printf("Waiting for file creation")

		err = waitForFileCreation(repomd)
		if err != nil {
			return fmt.Errorf("error while creating repository %s for source %s and destination %s", err.Error(), srcPath, destPath)
		}

		if err := exeCmd("gpg", "--batch", "--pinentry-mode=loopback", "--passphrase", conf.gpgPassphrase, "--detach-sign", "--armor", repomd); err != nil {
			return err
		}
		log.Printf("[✔] Uploading RPM succeded for src %s and dest %s \n", srcPath, destPath)
	}

	return nil
}

func uploadApt(conf config, srcTemplate string, upload Upload, arch string) error {

	// @TODO save snapshots

	for _, osVersion := range upload.OsVersion {
		log.Printf("[ ] Start uploading deb for os %s/%s", osVersion, arch)

		fileName, _ := replaceSrcDestTemplates(
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

		log.Printf("[ ] Create local repo for os %s/%s", osVersion, arch)
		// aptly repo create --distribution=${DISTRO} ${DISTRO}
		if err := exeCmd("aptly", "repo", "create", "--distribution=" + osVersion, osVersion); err != nil{
			return err
		}
		log.Printf("[✔] Local repo created for os %s/%s", osVersion, arch)

		// decide do we need to mirror ?
		//aptly mirror create -keyring=${GPG_KEYRING} mirror-${DISTRO} http://download.newrelic.com/infrastructure_agent/linux/apt ${DISTRO} main
		//aptly mirror update -keyring=${GPG_KEYRING} mirror-${DISTRO}
		//aptly repo import mirror-${DISTRO} ${DISTRO} Name


		log.Printf("[ ] Add package %s into deb repo for %s/%s", srcPath, osVersion, arch)
		// aptly repo add -force-replace=true ${DISTRO} ${srcPath}
		if err := exeCmd("aptly", "repo", "add", "-force-replace=true", osVersion, srcPath); err != nil{
			return err
		}
		log.Printf("[✔] Added succecfully package %s into deb repo for %s/%s", osVersion, arch)

		log.Printf("[ ] Publish deb repo for %s/%s", osVersion, arch)
		// aptly publish repo -gpg-key=${GPG_KEY_NAME} -keyring=${GPG_KEYRING} -passphrase-file=${GPG_KEY_PASSPHRASE} ${DISTRO}
		if err := exeCmd("aptly", "repo", "repo", "-gpg-key=${GPG_KEY_NAME}", "-keyring=${GPG_KEYRING}", "-passphrase-file=" + conf.gpgPassphrase, osVersion); err != nil{
			return err
		}
		log.Printf("[✔] Published succesfully deb repo for %s/%s", osVersion, arch)

		// TODO shoudl we sync once all repos updated?
		log.Printf("[ ] Sync local repo for %s/%s into s3", osVersion, arch)
		// ??? aws s3 sync aptly/public s3://nr-downloads-main/infrastructure_agent/test/linux/apt
		// do copy into s3 folder instead
		log.Printf("[✔] Synced succesfully local repo for %s/%s into s3", osVersion, arch)

	}

	return nil
}


// - exec tooling
// TODO handle command output as channel to see logs
// TODO command with context
// TODO add timeout to the command to avoid having it hanging

func exeCmd(name string, args ...string) error {
	cmd := exec.Command(name, args...)

	log.Printf("Executing in shell '%s'", cmd.String())
	output, err := cmd.CombinedOutput()
	log.Println(string(output))
	return err
}

// TODO remove?
func waitForFileCreation(repomd string) error {
	t := time.NewTicker(time.Second * 5)
	timeout := time.After(timeoutFileCreation)
	for {
		select {
		case <-t.C:
			_, err := os.Stat(repomd)
			if err == nil {
				return nil
			}
		case <-timeout:
			return fmt.Errorf("the repo creation failed for RPM")
		}
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

	err = copyFile(srcPath, destPath)
	if err != nil {
		return err
	}

	return nil
}

func copyFile(srcPath string, destPath string) (err error) {

	destDirectory := filepath.Dir(destPath)

	if _, err = os.Stat(destDirectory); os.IsNotExist(err) {
		// set right permissions
		err = os.MkdirAll(destDirectory, 0744)
		if err != nil {
			return err
		}
	}

	log.Println("[ ] Copy " + srcPath + " into " + destPath)
	input, err := ioutil.ReadFile(srcPath)
	if err != nil {
		return err
	}

	err = ioutil.WriteFile(destPath, input, 0744)
	if err != nil {
		return err
	}

	log.Println("[✔] Copy " + srcPath + " into " + destPath)
	return nil
}

func uploadArtifacts(conf config, schema uploadArtifactsSchema) error {
	for _, artifactSchema := range schema {

		err := uploadArtifact(conf, artifactSchema)
		if err != nil {
			return err
		}
	}
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

func downloadFile(url, destPath string) error {

	response, err := http.Get(url)
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
