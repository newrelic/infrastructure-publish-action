// Copyright 2021 New Relic Corporation. All rights reserved.
// SPDX-License-Identifier: Apache-2.0
package main

import (
	"bytes"
	"errors"
	"io"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"path"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/newrelic/infrastructure-publish-action/publisher/lock"
	"github.com/stretchr/testify/assert"
)

var (
	schemaValidMultipleEntries = ` 
- src: "foo.tar.gz"
  uploads:
    - type: file
      dest: /tmp
  arch:
    - amd64
    - 386
- src: "{integration_name}_linux_{version}_{arch}.tar.gz"
  uploads:
    - type: file
      dest: "infrastructure_agent/binaries/linux/{arch}/"
  arch:
    - ppc`

	schemaNoSrc = `
- uploads:
    - type: file
      dest: /tmp
  arch:
   - amd64
`
	schemaNoDest = `
- src: foo.tar.gz
  arch:
    - amd64
`
	schemaNoArch = `
- src: foo.tar.gz
  uploads:
    - type: file
      dest: /tmp
`
	schemaNotValid = `
- src: foo.tar.gz
  uploads: /tmp
`
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

// parse the configuration
func TestParseConfig(t *testing.T) {
	t.Parallel()
	tests := map[string]struct {
		schema string
		output []uploadArtifactSchema
	}{
		"multiple entries": {schemaValidMultipleEntries, []uploadArtifactSchema{
			{"foo.tar.gz", []string{"amd64", "386"}, []Upload{
				{
					Type: "file",
					Dest: "/tmp",
				},
			}},
			{"{integration_name}_linux_{version}_{arch}.tar.gz", []string{"ppc"}, []Upload{
				{
					Type: "file",
					Dest: "infrastructure_agent/binaries/linux/{arch}/",
				},
			}},
		}},
		"src is omitted": {schemaNoSrc, []uploadArtifactSchema{
			{"", []string{"amd64"}, []Upload{
				{
					Type: "file",
					Dest: "/tmp",
				},
			}},
		}},
		"arch is omitted": {schemaNoArch, []uploadArtifactSchema{
			{"foo.tar.gz", []string{""}, []Upload{
				{
					Type: "file",
					Dest: "/tmp",
				},
			}},
		}},
	}
	for name, tt := range tests {
		tt := tt
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			schema, err := parseUploadSchema([]byte(tt.schema))
			assert.NoError(t, err)
			assert.EqualValues(t, tt.output, schema)
		})
	}
}

// parse the configuration fails
func TestParseConfigError(t *testing.T) {
	t.Parallel()
	tests := map[string]string{
		"dest is omitted":      schemaNoDest,
		"dest is not an array": schemaNotValid,
	}
	for name, tt := range tests {
		tt := tt
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			schema, err := parseUploadSchema([]byte(tt))
			assert.Error(t, err)
			assert.Nil(t, schema)
		})
	}
}

func TestReplacePlaceholders(t *testing.T) {
	t.Parallel()
	tests := map[string]struct {
		srcTemplate  string
		destTemplate string
		repoName     string
		appName      string
		version      string
		arch         string
		srcOutput    string
		destOutput   string
		destPrefix   string
	}{
		"dst no file replacement": {
			"{app_name}-{arch}-{version}",
			"/{dest_prefix}/{arch}/{app_name}/{version}/file",
			"newrelic/nri-foobar",
			"nri-foobar",
			"1.2.3",
			"amd64",
			"nri-foobar-amd64-1.2.3",
			"/tmp/amd64/nri-foobar/1.2.3/file",
			"tmp",
		},
		"dst src replacement": {
			"{app_name}-{arch}-{version}",
			"/{dest_prefix}/{arch}/{app_name}/{version}/{src}",
			"newrelic/nri-foobar",
			"nri-foobar",
			"1.2.3",
			"amd64",
			"nri-foobar-amd64-1.2.3",
			"/tmp/amd64/nri-foobar/1.2.3/nri-foobar-amd64-1.2.3",
			"tmp"},
		"dst multiple replacements": {
			"{app_name}-{arch}-{version}",
			"/{dest_prefix}/{arch}/{app_name}/{version}/{app_name}-{arch}-{version}",
			"newrelic/nri-foobar",
			"nri-foobar",
			"1.2.3",
			"amd64",
			"nri-foobar-amd64-1.2.3",
			"/tmp/amd64/nri-foobar/1.2.3/nri-foobar-amd64-1.2.3",
			"tmp"},
		"src multiple replacements": {
			"{app_name}-{arch}-{version}-{app_name}-{arch}-{version}",
			"/{dest_prefix}/{arch}/{app_name}/{version}/file",
			"newrelic/nri-foobar",
			"nri-foobar",
			"1.2.3",
			"amd64",
			"nri-foobar-amd64-1.2.3-nri-foobar-amd64-1.2.3",
			"/tmp/amd64/nri-foobar/1.2.3/file",
			"tmp"},
	}
	for name, tt := range tests {
		tt := tt
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			tag := "v" + tt.version
			src, dest := replaceSrcDestTemplates(tt.srcTemplate, tt.destTemplate, "newrelic/foobar", tt.appName, tt.arch, tag, tt.version, tt.destPrefix, "")
			assert.EqualValues(t, tt.srcOutput, src)
			assert.EqualValues(t, tt.destOutput, dest)
		})
	}
}

func writeDummyFile(path string) error {
	file, err := os.Create(path)
	if err != nil {
		return err
	}
	defer file.Close()

	_, err = file.Write([]byte("test"))
	if err != nil {
		return err
	}

	return nil
}

func TestDownloadArtifacts(t *testing.T) {
	schema := []uploadArtifactSchema{
		{
			Src:  "{app_name}-{arch}-{version}-{os_version}.txt",
			Arch: []string{"amd64"},
			Uploads: []Upload{
				{
					Type:      "file",
					Dest:      "{arch}/{app_name}/{src}",
					OsVersion: []string{"os1", "os2"},
				},
			},
		},
	}

	cfg := config{
		version: "2.0.0",
		appName: "nri-foobar",
		urlTemplate: defaultUrlTemplate,
	}

	urlRecClient := newURLRecorderHTTPClient()
	err := newDownloader(urlRecClient).downloadArtifacts(cfg, schema)
	assert.NoError(t, err)

	expectedURLs := []string{"//releases/download//nri-foobar-amd64-2.0.0-os1.txt", "//releases/download//nri-foobar-amd64-2.0.0-os2.txt"}
	assert.Equal(t, expectedURLs, urlRecClient.urls)
}

func TestUploadArtifacts(t *testing.T) {
	schema := []uploadArtifactSchema{
		{"{app_name}-{arch}-{version}.txt", []string{"amd64", "386"}, []Upload{
			{
				Type: "file",
				Dest: "{arch}/{app_name}/{src}",
			},
		}},
		{"{app_name}-{arch}-{version}.txt", nil, []Upload{
			{
				Type: "file",
				Dest: "{arch}/{app_name}/{src}",
			},
		}},
	}

	dest := t.TempDir()
	src := t.TempDir()
	cfg := config{
		version:              "2.0.0",
		artifactsDestFolder:  dest,
		artifactsSrcFolder:   src,
		uploadSchemaFilePath: "",
		appName:              "nri-foobar",
	}

	err := writeDummyFile(path.Join(src, "nri-foobar-amd64-2.0.0.txt"))
	assert.NoError(t, err)

	err = writeDummyFile(path.Join(src, "nri-foobar-386-2.0.0.txt"))
	assert.NoError(t, err)

	err = uploadArtifacts(cfg, schema, lock.NewInMemory())
	assert.NoError(t, err)

	_, err = os.Stat(path.Join(dest, "amd64/nri-foobar/nri-foobar-amd64-2.0.0.txt"))
	assert.NoError(t, err)

	_, err = os.Stat(path.Join(dest, "386/nri-foobar/nri-foobar-386-2.0.0.txt"))
	assert.NoError(t, err)
}

func TestUploadArtifacts_cantBeRunInParallel(t *testing.T) {
	schema := []uploadArtifactSchema{
		{"{app_name}-{arch}-{version}.txt", []string{"amd64"}, []Upload{
			{
				Type: "file",
				Dest: "{arch}/{app_name}/{src}",
			},
		}},
		{"{app_name}-{arch}-{version}.txt", nil, []Upload{
			{
				Type: "file",
				Dest: "{arch}/{app_name}/{src}",
			},
		}},
	}

	dest := t.TempDir()
	src := t.TempDir()
	cfg := config{
		version:              "2.0.0",
		artifactsDestFolder:  dest,
		artifactsSrcFolder:   src,
		uploadSchemaFilePath: "",
		appName:              "nri-foobar",
	}

	err := writeDummyFile(path.Join(src, "nri-foobar-amd64-2.0.0.txt"))
	assert.NoError(t, err)

	ready := make(chan struct{})
	wg := sync.WaitGroup{}
	wg.Add(2)
	var err1, err2 error
	l := lock.NewInMemory()
	go func() {
		<-ready
		err1 = uploadArtifacts(cfg, schema, l)
		wg.Done()
	}()
	go func() {
		<-ready
		time.Sleep(1 * time.Millisecond)
		err2 = uploadArtifacts(cfg, schema, l)
		wg.Done()
	}()

	close(ready)
	wg.Wait()
	assert.NoError(t, err1)
	assert.Equal(t, lock.ErrLockBusy, err2, "2nd upload should fail because, 1st one got the lock")

	_, err = os.Stat(path.Join(dest, "amd64/nri-foobar/nri-foobar-amd64-2.0.0.txt"))
	assert.NoError(t, err)
}

func TestSchema(t *testing.T) {
	tests := []struct {
		name          string
		schemaPath    string
		expectedError error
	}{
		{"e2e", "../schemas/e2e.yml", nil},
		{"nrjmx", "../schemas/nrjmx.yml", nil},
		{"ohi", "../schemas/ohi.yml", nil},
		{"ohi-jmx", "../schemas/ohi-jmx.yml", nil},
		{"invalid yaml schema", "../test/schemas/bad-formatted-yaml.yml", errors.New("yaml: line 27: mapping values are not allowed in this context")},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			uploadSchemaContent, err := readFileContent(tt.schemaPath)

			uploadSchema, err := parseUploadSchema(uploadSchemaContent)
			assert.Equal(t, tt.expectedError, err)
			log.Println(uploadSchema)
		})
	}
}

func Test_streamAsLog(t *testing.T) {
	type args struct {
		content string
		prefix  string
	}
	tests := []struct {
		name string
		args args
	}{
		{"empty", args{"", ""}},
		{"empty with prefix", args{"", "some-prefix"}},
		{"content", args{"foo", ""}},
		{"content with prefix", args{"foo", "a-prefix"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var output bytes.Buffer
			l := log.New(&output, "", 0)

			wg := sync.WaitGroup{}
			wg.Add(1)
			streamAsLog(&wg, l, reader(tt.args.content), tt.args.prefix)

			assert.Equal(t, expectedLog(tt.args.prefix, tt.args.content), output.String())
		})
	}
}

func Test_execLogOutput_streamExecOutputEnabled(t *testing.T) {
	streamExecOutput = true

	tests := []struct {
		name        string
		cmdName     string
		cmdArgs     []string
		expectedLog string
		wantErr     bool
	}{
		{"empty", "", []string{}, "", true},
		{"echo stdout", "echo", []string{"foo"}, "stdout: foo", false},
		// pipes are being escaped, but function is shared btw stdout and stderr, so testing stdout should be enough
		//{"echo stderr", "echo", []string{"bar", ">>", "/dev/stderr"}, "stderr: bar", false},
	}
	var err error
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var output bytes.Buffer
			l := log.New(&output, "", 0)

			err = execLogOutput(l, tt.cmdName, tt.cmdArgs...)

			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				gotLog := output.String()
				assert.True(t, strings.Contains(gotLog, tt.expectedLog), ">> Logged lines:\n%s\n>> Don't contain: %s", gotLog, tt.expectedLog)
			}
		})
	}
}

func Test_generateDownloadUrl(t *testing.T) {

	repoName := "newrelic/infrastructure-agent"
	tag := "1.16.4"
	srcFile := "newrelic-infra-1.16.4-1.el8.arm.rpm"

	url := generateDownloadUrl(defaultUrlTemplate, repoName, tag, srcFile)

	assert.Equal(t, "https://github.com/newrelic/infrastructure-agent/releases/download/1.16.4/newrelic-infra-1.16.4-1.el8.arm.rpm", url)
}

func Test_generateAptSrcRepoUrl(t *testing.T) {
	template := "{access_point_host}/infrastructure_agent/linux/apt"
	accessPointHost := "https://download.newrelic.com"

	srcRepo := generateAptSrcRepoUrl(template, accessPointHost)

	assert.Equal(t, "https://download.newrelic.com/infrastructure_agent/linux/apt", srcRepo)
}

func Test_parseAccessPointHost(t *testing.T) {
	tests := []struct {
		name              string
		accessPointHost   string
		expectedUrl       string
		expectedMirrorUrl string
	}{
		{"empty value fallback to prod", "", accessPointProduction, mirrorProduction},
		{"production placeholder", "production", accessPointProduction, mirrorProduction},
		{"staging placeholder", "staging", accessPointStaging, accessPointStaging},
		{"testing placeholder", "testing", accessPointTesting, accessPointTesting},
		{"fixed url", "https://www.some-bucket-url.com", "https://www.some-bucket-url.com", "https://www.some-bucket-url.com"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srcRepo, mirrorHost := parseAccessPointHost(tt.accessPointHost)
			assert.Equal(t, tt.expectedUrl, srcRepo)
			assert.Equal(t, tt.expectedMirrorUrl, mirrorHost)
		})
	}
}

func Test_generateRepoFileContent(t *testing.T) {

	accessPointHost := "https://download.newrelic.com"
	repoPath := "infrastructure_agent/linux/apt"
	repoFileContent := generateRepoFileContent(accessPointHost, repoPath)

	expectedContent := "[newrelic-infra]\nname=New Relic Infrastructure\nbaseurl=https://download.newrelic.com/infrastructure_agent/linux/apt\ngpgkey=https://download.newrelic.com/infrastructure_agent/gpg/newrelic-infra.gpg\ngpgcheck=1\nrepo_gpgcheck=1"

	assert.Equal(t, expectedContent, repoFileContent)

}

func expectedLog(prefix, content string) string {
	if content == "" {
		return content
	}

	if prefix != "" {
		prefix += ": "
	}
	return prefix + content + "\n"
}

func reader(content string) io.ReadCloser {
	return ioutil.NopCloser(bytes.NewReader([]byte(content)))
}

func Test_loadConfig(t *testing.T) {
	tests := []struct {
		name string
		env  map[string]string
		want config
	}{
		{
			name: "defaults are applied",
			env: map[string]string{
				"TAG": "vFooBar",
			},
			want: config{
				tag:               "vFooBar",
				version:           "FooBar",
				accessPointHost:   accessPointProduction,
				mirrorHost:        mirrorProduction,
				aptlyFolder:       defaultAptlyFolder,
				lockGroup:         defaultLockgroup,
				urlTemplate:       defaultUrlTemplate,
				useDefLockRetries: true,
			},
		},
		{
			name: "custom values",
			env: map[string]string{
				"TAG":               "vFooBar",
				"APP_VERSION":       "Baz",
				"APTLY_FOLDER":      "FooFolder",
				"LOCK_GROUP":        "FooGroup",
				"ACCESS_POINT_HOST": "FooAPH",
				"LOCK_RETRIES":      "false",
				"URL_TEMPLATE":      "https://this/is/a/url/{repo/{tag}",
			},
			want: config{
				tag:               "vFooBar",
				version:           "Baz",
				accessPointHost:   "FooAPH",
				mirrorHost:        "FooAPH",
				aptlyFolder:       "FooFolder",
				lockGroup:         "FooGroup",
				urlTemplate:       "https://this/is/a/url/{repo/{tag}",
				useDefLockRetries: false,
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			for k, v := range tt.env {
				os.Setenv(k, v)
			}
			assert.Equal(t, tt.want, loadConfig(), "Case failed:", tt.name, tt.env)
		})
	}
}
