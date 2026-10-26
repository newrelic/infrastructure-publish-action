package config

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path"
	"strings"
)

const (
	schemaCustom      = "custom"
	schemaCustomLocal = "custom-local"
)

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
	// one app of the batch: no longer batch mode
	conf.AppsManifestPath = ""
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
