package config

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func Test_loadConfig_batchModeDoesNotNeedAppName(t *testing.T) {
	t.Setenv("APP_NAME", "")
	t.Setenv("APPS_MANIFEST_PATH", "/srv/manifest.json")

	conf, err := LoadConfig()
	require.NoError(t, err)
	assert.Equal(t, "/srv/manifest.json", conf.AppsManifestPath)
	assert.Equal(t, defaultSchemasFolder, conf.SchemasFolder)
}

func TestConfig_LockOwner(t *testing.T) {
	single := Config{AppName: "nri-apache", Tag: "v1.18.2", RunID: "123"}
	assert.Equal(t, "nri-apache_v1.18.2_123", single.LockOwner())

	batch := Config{AppsManifestPath: "/srv/manifest.json", RunID: "123"}
	assert.Equal(t, "batch_123", batch.LockOwner())
}

func Test_parseAppsManifest(t *testing.T) {
	specs, err := parseAppsManifest([]byte(`[
		{"app_name": "nri-apache", "repo_name": "newrelic/nri-apache", "tag": "v1.18.2", "schema": "ohi"},
		{"app_name": "nri-apache-fips", "repo_name": "newrelic/nri-apache", "tag": "v1.18.2", "schema": "ohi-fips"},
		{"app_name": "nri-kafka", "repo_name": "newrelic/nri-kafka", "tag": "v3.27.0", "schema": "custom", "schema_url": "http://foo/bar.yml"}
	]`))
	require.NoError(t, err)
	assert.Equal(t, []AppSpec{
		{AppName: "nri-apache", RepoName: "newrelic/nri-apache", Tag: "v1.18.2", Schema: "ohi"},
		{AppName: "nri-apache-fips", RepoName: "newrelic/nri-apache", Tag: "v1.18.2", Schema: "ohi-fips"},
		{AppName: "nri-kafka", RepoName: "newrelic/nri-kafka", Tag: "v3.27.0", Schema: "custom", SchemaURL: "http://foo/bar.yml"},
	}, specs)
}

func Test_parseAppsManifest_errors(t *testing.T) {
	tests := map[string]string{
		"not json":          `{`,
		"empty":             `[]`,
		"missing app_name":  `[{"repo_name": "newrelic/nri-apache", "tag": "v1.0.0", "schema": "ohi"}]`,
		"missing repo_name": `[{"app_name": "nri-apache", "tag": "v1.0.0", "schema": "ohi"}]`,
		"missing tag":       `[{"app_name": "nri-apache", "repo_name": "newrelic/nri-apache", "schema": "ohi"}]`,
		"missing schema":    `[{"app_name": "nri-apache", "repo_name": "newrelic/nri-apache", "tag": "v1.0.0"}]`,
		"duplicated app": `[
			{"app_name": "nri-apache", "repo_name": "newrelic/nri-apache", "tag": "v1.0.0", "schema": "ohi"},
			{"app_name": "nri-apache", "repo_name": "newrelic/nri-apache", "tag": "v1.0.1", "schema": "ohi"}]`,
	}
	for name, content := range tests {
		t.Run(name, func(t *testing.T) {
			_, err := parseAppsManifest([]byte(content))
			assert.Error(t, err)
		})
	}
}

func TestAppSpec_ToConfig(t *testing.T) {
	base := Config{AppsManifestPath: "/srv/manifest.json", DestPrefix: "infrastructure_agent/", RunID: "123", AccessPointHost: "http://host"}

	conf := AppSpec{AppName: "nri-mysql", RepoName: "newrelic/nri-mysql", Tag: "v1.25.0", Schema: "ohi"}.ToConfig(base)
	assert.Equal(t, Config{
		AppName:         "nri-mysql",
		RepoName:        "newrelic/nri-mysql",
		Tag:             "v1.25.0",
		Version:         "1.25.0",
		Schema:          "ohi",
		DestPrefix:      "infrastructure_agent/",
		RunID:           "123",
		AccessPointHost: "http://host",
	}, conf)
	assert.Equal(t, "/srv/manifest.json", base.AppsManifestPath, "base config must not change")
	assert.Equal(t, "nri-mysql_v1.25.0_123", conf.LockOwner(), "an app of the batch has a single-app identity")

	conf = AppSpec{AppName: "nri-mysql", RepoName: "newrelic/nri-mysql", Tag: "v1.25.0", AppVersion: "1.25.0-rc", Schema: "ohi"}.ToConfig(base)
	assert.Equal(t, "1.25.0-rc", conf.Version)
}

func TestResolveSchemaFile(t *testing.T) {
	schemas := t.TempDir()
	require.NoError(t, os.WriteFile(path.Join(schemas, "ohi.yml"), []byte("ohi"), 0644))
	local := path.Join(t.TempDir(), "local.yml")
	require.NoError(t, os.WriteFile(local, []byte("local"), 0644))

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/schema.yml" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		_, _ = w.Write([]byte("remote"))
	}))
	defer server.Close()

	tests := []struct {
		name    string
		spec    AppSpec
		content string
		wantErr bool
	}{
		{name: "baked-in schema", spec: AppSpec{AppName: "nri-apache", Schema: "ohi"}, content: "ohi"},
		{name: "unknown schema", spec: AppSpec{AppName: "nri-apache", Schema: "nope"}, wantErr: true},
		{name: "custom-local", spec: AppSpec{AppName: "nri-apache", Schema: "custom-local", SchemaPath: local}, content: "local"},
		{name: "custom-local missing", spec: AppSpec{AppName: "nri-apache", Schema: "custom-local", SchemaPath: "/nope.yml"}, wantErr: true},
		{name: "custom", spec: AppSpec{AppName: "nri-apache", Schema: "custom", SchemaURL: server.URL + "/schema.yml"}, content: "remote"},
		{name: "custom not found", spec: AppSpec{AppName: "nri-apache", Schema: "custom", SchemaURL: server.URL + "/nope.yml"}, wantErr: true},
		{name: "custom without url", spec: AppSpec{AppName: "nri-apache", Schema: "custom"}, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			schemaFile, err := ResolveSchemaFile(tt.spec, schemas, t.TempDir(), server.Client())
			if tt.wantErr {
				assert.Error(t, err)
				return
			}
			require.NoError(t, err)
			content, err := os.ReadFile(schemaFile)
			require.NoError(t, err)
			assert.Equal(t, tt.content, string(content))
		})
	}
}
