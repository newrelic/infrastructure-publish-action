package main

import (
	"github.com/stretchr/testify/assert"
	"log"
	"os"
	"path"
	"testing"
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

	err = uploadArtifacts(cfg, schema)
	assert.NoError(t, err)

	_, err = os.Stat(path.Join(dest, "amd64/nri-foobar/nri-foobar-amd64-2.0.0.txt"))
	assert.NoError(t, err)

	_, err = os.Stat(path.Join(dest, "386/nri-foobar/nri-foobar-386-2.0.0.txt"))
	assert.NoError(t, err)
}

func TestSchema(t *testing.T) {
	uploadSchemaContent, err := readFileContent("../schemas/nrjmx.yml")

	uploadSchema, err := parseUploadSchema(uploadSchemaContent)

	if err != nil {
		log.Fatal(err)
	}
	log.Println(uploadSchema)
}
