package upload

import (
	"github.com/newrelic/infrastructure-publish-action/publisher/config"
	"github.com/newrelic/infrastructure-publish-action/publisher/lock"
	"github.com/stretchr/testify/assert"
	"os"
	"path"
	"sync"
	"testing"
	"time"
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

func TestUploadArtifacts(t *testing.T) {
	schema := []config.UploadArtifactSchema{
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
	}

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

	err = UploadArtifacts(cfg, schema, lock.NewInMemory())
	assert.NoError(t, err)

	_, err = os.Stat(path.Join(dest, "amd64/nri-foobar/nri-foobar-amd64-2.0.0.txt"))
	assert.NoError(t, err)

	_, err = os.Stat(path.Join(dest, "386/nri-foobar/nri-foobar-386-2.0.0.txt"))
	assert.NoError(t, err)
}

func TestUploadArtifacts_cantBeRunInParallel(t *testing.T) {
	schema := []config.UploadArtifactSchema{
		{"{app_name}-{arch}-{version}.txt", []string{"amd64"}, []config.Upload{
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
	}

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

	ready := make(chan struct{})
	wg := sync.WaitGroup{}
	wg.Add(2)
	var err1, err2 error
	l := lock.NewInMemory()
	go func() {
		<-ready
		err1 = UploadArtifacts(cfg, schema, l)
		wg.Done()
	}()
	go func() {
		<-ready
		time.Sleep(1 * time.Millisecond)
		err2 = UploadArtifacts(cfg, schema, l)
		wg.Done()
	}()

	close(ready)
	wg.Wait()
	assert.NoError(t, err1)
	assert.Equal(t, lock.ErrLockBusy, err2, "2nd upload should fail because, 1st one got the lock")

	_, err = os.Stat(path.Join(dest, "amd64/nri-foobar/nri-foobar-amd64-2.0.0.txt"))
	assert.NoError(t, err)
}

func TestUploadArtifacts_errorsIfAnyArchFails(t *testing.T) {
	schema := []config.UploadArtifactSchema{
		{"{app_name}-{arch}-{version}.txt", []string{"amd64"}, []config.Upload{
			{
				Type: "file",
				Dest: "{arch}/{app_name}/{src}",
			},
		}},
		{"{app_name}-{arch}-{version}.txt", []string{"amd64", "386"}, []config.Upload{
			{
				Type: "file",
				Dest: "{arch}/{app_name}/{src}",
			},
		}},
	}

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

	ready := make(chan struct{})
	wg := sync.WaitGroup{}
	wg.Add(1)
	var err1 error
	l := lock.NewInMemory()
	go func() {
		<-ready
		err1 = UploadArtifacts(cfg, schema, l)
		wg.Done()
	}()

	close(ready)
	wg.Wait()
	assert.Error(t, err1)
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

	expectedContent := "[newrelic-infra]\nname=New Relic Infrastructure\nbaseurl=https://download.newrelic.com/infrastructure_agent/linux/apt\ngpgkey=https://download.newrelic.com/infrastructure_agent/gpg/newrelic-infra.gpg\ngpgcheck=1\nrepo_gpgcheck=1"

	assert.Equal(t, expectedContent, repoFileContent)

}
