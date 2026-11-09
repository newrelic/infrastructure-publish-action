package main

import (
	"encoding/json"
	"errors"
	"os"
	"path"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/newrelic/infrastructure-publish-action/publisher/config"
	"github.com/newrelic/infrastructure-publish-action/publisher/lock"
	"github.com/newrelic/infrastructure-publish-action/publisher/release"
	"github.com/newrelic/infrastructure-publish-action/publisher/upload"
)

const testSchema = `
- src: "{app_name}-{arch}-{version}.txt"
  arch: [amd64]
  uploads:
    - type: file
      dest: "{arch}/{src}"
`

// fakeDownloader fails for the apps listed in failing.
type fakeDownloader struct {
	failing map[string]error
	called  []string
}

func (f *fakeDownloader) DownloadArtifacts(conf config.Config, _ config.UploadArtifactSchemas) error {
	f.called = append(f.called, conf.AppName)
	return f.failing[conf.AppName]
}

func schemasFolder(t *testing.T) string {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(path.Join(dir, "test.yml"), []byte(testSchema), 0644))
	return dir
}

func spec(app string) config.AppSpec {
	return config.AppSpec{AppName: app, RepoName: "newrelic/" + app, Tag: "v1.0.0", Schema: "test"}
}

func TestPrepareBatch_skipsTheAppsThatFailInsteadOfTheWholeBatch(t *testing.T) {
	d := &fakeDownloader{failing: map[string]error{"nri-bad": errors.New("status code 404")}}
	opts := config.BatchOptions{SchemasFolder: schemasFolder(t)}
	missingSchema := spec("nri-noschema")
	missingSchema.Schema = "nope"

	jobs, skipped := prepareBatch(config.Config{}, opts,
		[]config.AppSpec{spec("nri-good"), spec("nri-bad"), missingSchema, spec("nri-good-fips")}, d)

	require.Len(t, jobs, 2)
	assert.Equal(t, "nri-good", jobs[0].Conf.AppName)
	assert.Equal(t, "nri-good-fips", jobs[1].Conf.AppName)
	require.Len(t, skipped, 2)
	assert.Equal(t, "nri-bad", skipped[0].AppName)
	assert.Equal(t, "download: status code 404", skipped[0].Reason)
	assert.Equal(t, "nri-noschema", skipped[1].AppName)
	assert.Contains(t, skipped[1].Reason, "nope.yml")
	assert.NotContains(t, d.called, "nri-noschema", "an app without schema is not downloaded")
}

func TestPrepareBatch_localPackagesAreNotDownloaded(t *testing.T) {
	d := &fakeDownloader{}
	opts := config.BatchOptions{SchemasFolder: schemasFolder(t)}

	jobs, skipped := prepareBatch(config.Config{LocalPackagesPath: "/srv/pkgs"}, opts, []config.AppSpec{spec("nri-good")}, d)

	require.Len(t, jobs, 1)
	assert.Empty(t, skipped)
	assert.Empty(t, d.called)
	assert.Equal(t, "/srv/pkgs", jobs[0].Conf.ArtifactsSrcFolder)
}

type unusedMarker struct{ release.BatchMarker }

func TestRunBatch_nothingToPublishFailsAndReportsWhy(t *testing.T) {
	manifest := path.Join(t.TempDir(), "manifest.json")
	require.NoError(t, os.WriteFile(manifest, []byte(`[{"app_name":"nri-bad","repo_name":"newrelic/nri-bad","tag":"v1.0.0","schema":"nope"}]`), 0644))
	opts := config.BatchOptions{ManifestPath: manifest, SchemasFolder: schemasFolder(t), ResultPath: path.Join(path.Dir(manifest), "batch-result.json")}

	result, err := runBatch(config.Config{}, opts, lock.NewNoop(), unusedMarker{})

	assert.ErrorIs(t, err, upload.ErrEmptyBatch)
	require.NoError(t, result.write(opts.ResultPath))
	content, err := os.ReadFile(opts.ResultPath)
	require.NoError(t, err)
	var got map[string]interface{}
	require.NoError(t, json.Unmarshal(content, &got))
	assert.Equal(t, "failed", got["status"])
	assert.Equal(t, []interface{}{}, got["published"])
	assert.Len(t, got["skipped"], 1)
}

func TestBatchResult_write(t *testing.T) {
	file := path.Join(t.TempDir(), "r.json")
	r := batchResult{
		Status:    statusPublished,
		Published: []appRef{{AppName: "nri-apache", RepoName: "newrelic/nri-apache", Tag: "v1.18.3"}},
		Skipped:   []skippedApp{{appRef: appRef{AppName: "nri-mysql", RepoName: "newrelic/nri-mysql", Tag: "v1.26.2"}, Reason: "download: 404"}},
	}
	require.NoError(t, r.write(file))

	content, err := os.ReadFile(file)
	require.NoError(t, err)
	assert.JSONEq(t, `{
		"status": "published",
		"published": [{"app_name": "nri-apache", "repo_name": "newrelic/nri-apache", "tag": "v1.18.3"}],
		"skipped": [{"app_name": "nri-mysql", "repo_name": "newrelic/nri-mysql", "tag": "v1.26.2", "reason": "download: 404"}]
	}`, string(content))
	assert.True(t, strings.HasSuffix(string(content), "\n"))
}

func Test_markerDirectory(t *testing.T) {
	assert.Equal(t, "infrastructure_agent", markerDirectory("infrastructure_agent/"))
	assert.Equal(t, "infrastructure_agent_v2", markerDirectory("/infrastructure_agent_v2/"))
	assert.Equal(t, "nested/prefix/infrastructure_agent_v2",
		markerDirectory("nested/prefix/infrastructure_agent_v2/"))
}
