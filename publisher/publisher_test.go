package main

import (
	"os"
	"path"
	"sync"
	"testing"
	"time"

	"github.com/newrelic/infrastructure-publish-action/publisher/config"
	"github.com/newrelic/infrastructure-publish-action/publisher/lock"
	"github.com/stretchr/testify/assert"
)

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
		err1 = UploadAndClean(cfg, schema, l)
		wg.Done()
	}()
	go func() {
		<-ready
		time.Sleep(1 * time.Millisecond)
		err2 = UploadAndClean(cfg, schema, l)
		wg.Done()
	}()

	close(ready)
	wg.Wait()
	assert.NoError(t, err1)
	assert.Equal(t, lock.ErrLockBusy, err2, "2nd upload should fail because, 1st one got the lock")

	_, err = os.Stat(path.Join(dest, "amd64/nri-foobar/nri-foobar-amd64-2.0.0.txt"))
	assert.NoError(t, err)
}
