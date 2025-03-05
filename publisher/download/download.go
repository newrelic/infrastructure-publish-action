package download

import (
	"fmt"
	"github.com/newrelic/infrastructure-publish-action/publisher/config"
	"github.com/newrelic/infrastructure-publish-action/publisher/utils"
	"io"
	"net/http"
	"os"
	"path"
	"strings"
	"time"
)

const (
	urlTemplate        = "https://github.com/{repo_name}/releases/download/{tag}/{src}"
	retries            = 5
	durationAfterRetry = 2 * time.Second
)

func (d *downloader) downloadArtifact(conf config.Config, src, arch, osVersion string) error {

	utils.Logger.Println("Starting downloading artifacts!")

	srcFile := utils.ReplacePlaceholders(src, conf.RepoName, conf.AppName, arch, conf.Tag, conf.Version, conf.DestPrefix, osVersion)
	url := generateDownloadUrl(urlTemplate, conf.RepoName, conf.Tag, srcFile)

	destPath := path.Join(conf.ArtifactsSrcFolder, srcFile)

	utils.Logger.Println(fmt.Sprintf("[ ] Download %s into %s", url, destPath))

	err := utils.Retry(
		func() error {
			return d.downloadFile(url, destPath)
		},
		retries,
		durationAfterRetry,
		func() {
			utils.Logger.Printf("retrying downloadFile %s\n", url)
		})

	if err != nil {
		return err
	}

	fi, err := os.Stat(destPath)
	if err != nil {
		return err
	}

	utils.Logger.Println(fmt.Sprintf("[✔] Download %s into %s %d bytes", url, destPath, fi.Size()))

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

func NewDownloader(client HTTPClient) *downloader {
	return &downloader{
		httpClient: client,
	}
}

func (d *downloader) DownloadArtifacts(conf config.Config, schema config.UploadArtifactSchemas) error {
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

func GenerateDownloadFileName(srcFileTemplate, repoName, appName, arch, tag, version, destPrefix, osVersion string) string {
	return utils.ReplacePlaceholders(srcFileTemplate, repoName, appName, arch, tag, version, destPrefix, osVersion)
}

func generateDownloadUrl(template, repoName, tag, srcFile string) (url string) {
	url = utils.ReplacePlaceholders(template, repoName, "", "", tag, "", "", "")
	url = strings.Replace(url, utils.PlaceholderForSrc, srcFile, -1)

	return
}
