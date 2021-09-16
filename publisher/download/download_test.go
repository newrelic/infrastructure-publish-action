package download

import (
	"bytes"
	"github.com/newrelic/infrastructure-publish-action/publisher/config"
	"github.com/stretchr/testify/assert"
	"io/ioutil"
	"net/http"
	"testing"
)

type urlRecorderHTTPClient struct {
	urls []string
}

func newURLRecorderHTTPClient() *urlRecorderHTTPClient {
	return &urlRecorderHTTPClient{
		urls: []string{},
	}
}

func (c *urlRecorderHTTPClient) Do(req *http.Request) (*http.Response, error) {
	c.urls = append(c.urls, req.URL.Path)

	return &http.Response{
		StatusCode: http.StatusOK,
		Body:       ioutil.NopCloser(bytes.NewReader([]byte{})),
	}, nil
}


func TestDownloadArtifacts(t *testing.T) {
	schema := []config.UploadArtifactSchema{
		{
			Src:  "{app_name}-{arch}-{version}-{os_version}.txt",
			Arch: []string{"amd64"},
			Uploads: []config.Upload{
				{
					Type:      "file",
					Dest:      "{arch}/{app_name}/{src}",
					OsVersion: []string{"os1", "os2"},
				},
			},
		},
	}

	cfg := config.Config{
		Version: "2.0.0",
		AppName: "nri-foobar",
	}

	urlRecClient := newURLRecorderHTTPClient()
	err := NewDownloader(urlRecClient).DownloadArtifacts(cfg, schema)
	assert.NoError(t, err)

	expectedURLs := []string{"//releases/download//nri-foobar-amd64-2.0.0-os1.txt", "//releases/download//nri-foobar-amd64-2.0.0-os2.txt"}
	assert.Equal(t, expectedURLs, urlRecClient.urls)
}

func Test_generateDownloadUrl(t *testing.T) {

	repoName := "newrelic/infrastructure-agent"
	tag := "1.16.4"
	srcFile := "newrelic-infra-1.16.4-1.el8.arm.rpm"

	url := generateDownloadUrl(urlTemplate, repoName, tag, srcFile)

	assert.Equal(t, "https://github.com/newrelic/infrastructure-agent/releases/download/1.16.4/newrelic-infra-1.16.4-1.el8.arm.rpm", url)
}
