package config

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path"
	"strings"

	"github.com/spf13/viper"
)

const (
	schemaCustom      = "custom"
	schemaCustomLocal = "custom-local"
)

const (
	defaultSchemasFolder = "/home/gha/schemas"
	// A batch has no single app: it is identified by its run id in the lock owner and release markers.
	batchAppName = "batch"
	batchTag     = "manifest"
)

// BatchOptions are the settings that only exist in batch mode.
type BatchOptions struct {
	ManifestPath  string
	SchemasFolder string
	// ResultPath is where the batch result JSON is written
	ResultPath string
}

// LoadBatch loads the base config shared by every app of a batch, the same way LoadConfig does
// for a single app, plus the batch settings. Only the manifest path is required: app_name, repo_name,
// tag and schema come from every manifest entry.
func LoadBatch() (Config, BatchOptions, error) {
	viper.BindEnv("apps_manifest_path")
	viper.BindEnv("schemas_folder")
	viper.BindEnv("batch_result_path")

	manifestPath := viper.GetString("apps_manifest_path")
	if manifestPath == "" {
		return Config{}, BatchOptions{}, fmt.Errorf("%w: apps_manifest_path", ErrMissingConfig)
	}
	schemasFolder := viper.GetString("schemas_folder")
	if schemasFolder == "" {
		schemasFolder = defaultSchemasFolder
	}

	// LoadConfig requires an app name: give it the batch identity unless the env sets one
	// (viper is global: the defaults are cleared again so they don't leak into a later LoadConfig)
	viper.SetDefault("app_name", batchAppName)
	viper.SetDefault("tag", batchTag)
	defer viper.SetDefault("app_name", "")
	defer viper.SetDefault("tag", "")
	conf, err := LoadConfig()
	if err != nil {
		return Config{}, BatchOptions{}, err
	}
	resultPath := viper.GetString("batch_result_path")
	if resultPath == "" {
		// next to the manifest, which is the folder shared with the host
		resultPath = path.Join(path.Dir(manifestPath), "batch-result.json")
	}
	return conf, BatchOptions{ManifestPath: manifestPath, SchemasFolder: schemasFolder, ResultPath: resultPath}, nil
}

// AppSpec is one entry of the batch manifest. It carries the same values a single-app run
// gets through the action inputs (app_name, repo_name, tag, schema...).
type AppSpec struct {
	AppName    string `json:"app_name"`
	RepoName   string `json:"repo_name"`
	Tag        string `json:"tag"`
	AppVersion string `json:"app_version,omitempty"`
	Schema     string `json:"schema"`
	SchemaURL  string `json:"schema_url,omitempty"`
	SchemaPath string `json:"schema_path,omitempty"`
}

// ParseAppsManifest reads the JSON array of apps to publish in one batch.
func ParseAppsManifest(manifestPath string) ([]AppSpec, error) {
	content, err := os.ReadFile(manifestPath)
	if err != nil {
		return nil, err
	}
	return parseAppsManifest(content)
}

func parseAppsManifest(content []byte) ([]AppSpec, error) {
	var specs []AppSpec
	if err := json.Unmarshal(content, &specs); err != nil {
		return nil, fmt.Errorf("parsing apps manifest: %w", err)
	}
	if len(specs) == 0 {
		return nil, fmt.Errorf("%w: apps manifest is empty", ErrMissingConfig)
	}

	seen := map[string]bool{}
	for i, spec := range specs {
		for field, value := range map[string]string{"app_name": spec.AppName, "repo_name": spec.RepoName, "tag": spec.Tag, "schema": spec.Schema} {
			if value == "" {
				return nil, fmt.Errorf("%w: %s in apps manifest entry %d", ErrMissingConfig, field, i)
			}
		}
		if seen[spec.AppName] {
			return nil, fmt.Errorf("app %s is listed twice in the apps manifest", spec.AppName)
		}
		seen[spec.AppName] = true
	}
	return specs, nil
}

// ToConfig returns the config a single-app run would get for this app.
func (a AppSpec) ToConfig(base Config) Config {
	conf := base
	conf.AppName = a.AppName
	conf.RepoName = a.RepoName
	conf.Tag = a.Tag
	conf.Schema = a.Schema
	conf.SchemaURL = a.SchemaURL
	conf.Version = a.AppVersion
	if conf.Version == "" {
		// same derivation as LoadConfig
		conf.Version = strings.Replace(a.Tag, "v", "", -1)
	}
	return conf
}

// ResolveSchemaFile returns the schema file for the app, like the Makefile's prepare-schema
// target does for a single-app run.
func ResolveSchemaFile(spec AppSpec, schemasFolder, downloadFolder string, client *http.Client) (string, error) {
	switch spec.Schema {
	case schemaCustom:
		if spec.SchemaURL == "" {
			return "", fmt.Errorf("%w: schema_url for app %s", ErrMissingConfig, spec.AppName)
		}
		resp, err := client.Get(spec.SchemaURL)
		if err != nil {
			return "", err
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			return "", fmt.Errorf("downloading schema %s: status code %d", spec.SchemaURL, resp.StatusCode)
		}
		schemaPath := path.Join(downloadFolder, spec.AppName+"-custom.yml")
		f, err := os.Create(schemaPath)
		if err != nil {
			return "", err
		}
		defer f.Close()
		if _, err = io.Copy(f, resp.Body); err != nil {
			return "", err
		}
		return schemaPath, nil
	case schemaCustomLocal:
		if _, err := os.Stat(spec.SchemaPath); err != nil {
			return "", fmt.Errorf("cannot find schema_path %q for app %s: %w", spec.SchemaPath, spec.AppName, err)
		}
		return spec.SchemaPath, nil
	default:
		schemaPath := path.Join(schemasFolder, spec.Schema+".yml")
		if _, err := os.Stat(schemaPath); err != nil {
			return "", fmt.Errorf("cannot find %s.yml file in %s", spec.Schema, schemasFolder)
		}
		return schemaPath, nil
	}
}
