package main

import (
	"github.com/stretchr/testify/assert"
	"os"
	"path"
	"testing"
)

var (
	schema = ` 
- src: "foo.tar.gz"
  dest: 
    - /tmp
  arch:
    - amd64
    - 386
- src: "{integration_name}_linux_{version}_{arch}.tar.gz"
  dest: 
    - "infrastructure_agent/binaries/linux/{arch}/"
  arch:
    - ppc`

	schemaNoSrc = `
- dest: 
    - /tmp
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
  dest: 
    - /tmp
`
	schemaNotValid = `
- src: foo.tar.gz
  dest: /tmp
`
)

// parse the configuration
func TestParseConfig(t *testing.T) {
	t.Parallel()
	tests := map[string]struct {
		schema string
		output []uploadArtifactSchema
	}{
		"multiple entries": {schema, []uploadArtifactSchema{
			{"foo.tar.gz", []string{"/tmp"}, []string{"amd64", "386"}},
			{"{integration_name}_linux_{version}_{arch}.tar.gz", []string{"infrastructure_agent/binaries/linux/{arch}/"}, []string{"ppc"}},
		}},
		"src is omitted": {schemaNoSrc, []uploadArtifactSchema{
			{"", []string{"/tmp"}, []string{"amd64"}},
		}},
		"arch is omitted": {schemaNoArch, []uploadArtifactSchema{
			{"foo.tar.gz", []string{"/tmp"}, []string{""}},
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
			src, dest := replaceSrcDestTemplates(tt.srcTemplate, tt.destTemplate, "newrelic/foobar", tt.appName, tt.arch, tag, tt.version, tt.destPrefix)
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
	schema := []uploadArtifactSchema{
		{"{app_name}-{arch}-{version}.txt", []string{"{arch}/{app_name}/{src}"}, []string{"amd64", "386"}},
		{"{app_name}-{arch}-{version}.txt", []string{"{arch}/{app_name}/{src}"}, nil},
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

	err = uploadArtifacts(cfg, schema)
	assert.NoError(t, err)

	_, err = os.Stat(path.Join(dest, "amd64/nri-foobar/nri-foobar-amd64-2.0.0.txt"))
	assert.NoError(t, err)

	_, err = os.Stat(path.Join(dest, "386/nri-foobar/nri-foobar-386-2.0.0.txt"))
	assert.NoError(t, err)
}
