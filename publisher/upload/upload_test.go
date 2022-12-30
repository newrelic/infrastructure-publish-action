package upload

import (
	"io/ioutil"
	"os"
	"path"
	"testing"

	"github.com/newrelic/infrastructure-publish-action/publisher/config"
	"github.com/stretchr/testify/assert"
)

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
		osVersion    string
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
			"",
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
			"tmp",
			"",
		},
		"dst multiple replacements": {
			"{app_name}-{arch}-{version}",
			"/{dest_prefix}/{arch}/{app_name}/{version}/{app_name}-{arch}-{version}",
			"newrelic/nri-foobar",
			"nri-foobar",
			"1.2.3",
			"amd64",
			"nri-foobar-amd64-1.2.3",
			"/tmp/amd64/nri-foobar/1.2.3/nri-foobar-amd64-1.2.3",
			"tmp",
			"",
		},
		"src and dst multiple replacements with os_version": {
			"{app_name}-{arch}-{version}-{app_name}-{arch}-{version}-{os_version}",
			"/{dest_prefix}/{arch}/{app_name}/{version}/{os_version}/file",
			"newrelic/nri-foobar",
			"nri-foobar",
			"1.2.3",
			"amd64",
			"nri-foobar-amd64-1.2.3-nri-foobar-amd64-1.2.3-22",
			"/tmp/amd64/nri-foobar/1.2.3/22/file",
			"tmp",
			"22",
		},
	}
	for name, tt := range tests {
		tt := tt
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			tag := "v" + tt.version
			src, dest := replaceSrcDestTemplates(tt.srcTemplate, tt.destTemplate, "newrelic/foobar", tt.appName, tt.arch, tag, tt.version, tt.destPrefix, tt.osVersion)
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

func TestUploadArtifacts(t *testing.T) {
	var testArtifacts = []struct {
		name          string
		schema        []config.UploadArtifactSchema
		dummyFiles    []string
		expectedFiles []string
	}{
		{
			name: "AppName, arch and app version expansion",
			schema: []config.UploadArtifactSchema{
				{"{app_name}-{arch}-{version}.txt", []string{"amd64", "386"}, []config.Upload{
					{
						Type: "file",
						Dest: "{arch}/{app_name}/{src}",
					},
				}},
				{"{app_name}-{arch}-{version}.txt", nil, []config.Upload{
					{
						Type: "file",
						Dest: "{arch}/{app_name}/{src}",
					},
				}},
			},
			dummyFiles:    []string{"nri-foobar-amd64-2.0.0.txt", "nri-foobar-386-2.0.0.txt"},
			expectedFiles: []string{"amd64/nri-foobar/nri-foobar-amd64-2.0.0.txt", "386/nri-foobar/nri-foobar-386-2.0.0.txt"},
		},
		{
			name: "AppName, arch, app version and os version expansion",
			schema: []config.UploadArtifactSchema{
				{"{app_name}-{version}-1.amazonlinux-{os_version}.{arch}.rpm.sum", []string{"x86_64"}, []config.Upload{
					{
						Type:      "file",
						Dest:      "{arch}/{app_name}/{os_version}/{src}",
						OsVersion: []string{"2", "2022"},
					},
				}},
			},
			dummyFiles:    []string{"nri-foobar-2.0.0-1.amazonlinux-2.x86_64.rpm.sum", "nri-foobar-2.0.0-1.amazonlinux-2022.x86_64.rpm.sum"},
			expectedFiles: []string{"x86_64/nri-foobar/2/nri-foobar-2.0.0-1.amazonlinux-2.x86_64.rpm.sum", "x86_64/nri-foobar/2022/nri-foobar-2.0.0-1.amazonlinux-2022.x86_64.rpm.sum"},
		},
	}

	dest, err := ioutil.TempDir("", "")
	assert.NoError(t, err)
	src, err := ioutil.TempDir("", "")
	assert.NoError(t, err)

	cfg := config.Config{
		Version:              "2.0.0",
		ArtifactsDestFolder:  dest,
		ArtifactsSrcFolder:   src,
		UploadSchemaFilePath: "",
		AppName:              "nri-foobar",
	}

	for _, artifact := range testArtifacts {
		t.Run(artifact.name, func(t *testing.T) {
			for _, dummyFile := range artifact.dummyFiles {
				err := writeDummyFile(path.Join(src, dummyFile))
				assert.NoError(t, err)
			}
			err := UploadArtifacts(cfg, artifact.schema)
			assert.NoError(t, err)

			for _, expectedFile := range artifact.expectedFiles {
				_, err = os.Stat(path.Join(dest, expectedFile))
				assert.NoError(t, err)
			}
		})
	}

}

func TestUploadArtifacts_errorsIfAnyArchFails(t *testing.T) {
	tests := []struct {
		name         string
		schema       []config.UploadArtifactSchema
		expectsError bool
	}{
		{
			name: "no error uploading file",
			schema: []config.UploadArtifactSchema{
				{"{app_name}-{arch}-{version}.txt", []string{"amd64", "386"}, []config.Upload{
					{
						Type: "file",
						Dest: "{arch}/{app_name}/{src}",
					},
				}},
			},
		},
		{
			name: "error uploading file",
			schema: []config.UploadArtifactSchema{
				{"{app_name}-{arch}-{version}.txt", []string{"amd64", "NOT_VALID", "386"}, []config.Upload{
					{
						Type: "file",
						Dest: "{arch}/{app_name}/{src}",
					},
				}},
			},
			expectsError: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			dest := os.TempDir()
			src := os.TempDir()
			cfg := config.Config{
				Version:              "2.0.0",
				ArtifactsDestFolder:  dest,
				ArtifactsSrcFolder:   src,
				UploadSchemaFilePath: "",
				AppName:              "nri-foobar",
			}

			err := writeDummyFile(path.Join(src, "nri-foobar-amd64-2.0.0.txt"))
			assert.NoError(t, err)
			err = writeDummyFile(path.Join(src, "nri-foobar-386-2.0.0.txt"))
			assert.NoError(t, err)

			err = UploadArtifacts(cfg, tc.schema)
			if tc.expectsError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func Test_generateAptSrcRepoUrl(t *testing.T) {
	template := "{access_point_host}/infrastructure_agent/linux/apt"
	accessPointHost := "https://download.newrelic.com"

	srcRepo := generateAptSrcRepoUrl(template, accessPointHost)

	assert.Equal(t, "https://download.newrelic.com/infrastructure_agent/linux/apt", srcRepo)
}

func Test_generateRepoFileContent(t *testing.T) {

	accessPointHost := "https://download.newrelic.com"
	repoPath := "infrastructure_agent/linux/apt"
	repoFileContent := generateRepoFileContent(accessPointHost, repoPath)

	expectedContent := "[newrelic-infra]\nname=New Relic Infrastructure\nbaseurl=https://download.newrelic.com/infrastructure_agent/linux/apt\ngpgkey=https://download.newrelic.com/infrastructure_agent/keys/newrelic_rpm_key_current.gpg\ngpgcheck=1\nrepo_gpgcheck=1"

	assert.Equal(t, expectedContent, repoFileContent)

}
