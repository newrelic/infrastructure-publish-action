package config

import (
	"errors"
	"github.com/stretchr/testify/assert"
	"log"
	"testing"
)

const (
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
func TestParseSchema(t *testing.T) {
	t.Parallel()
	tests := map[string]struct {
		schema string
		output []UploadArtifactSchema
	}{
		"multiple entries": {schemaValidMultipleEntries, []UploadArtifactSchema{
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
		"src is omitted": {schemaNoSrc, []UploadArtifactSchema{
			{"", []string{"amd64"}, []Upload{
				{
					Type: "file",
					Dest: "/tmp",
				},
			}},
		}},
		"arch is omitted": {schemaNoArch, []UploadArtifactSchema{
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

func TestSchema(t *testing.T) {
	tests := []struct {
		name          string
		schemaPath    string
		expectedError error
	}{
		{"e2e", "../../schemas/e2e.yml", nil},
		{"nrjmx", "../../schemas/nrjmx.yml", nil},
		{"ohi", "../../schemas/ohi.yml", nil},
		{"ohi-jmx", "../../schemas/ohi-jmx.yml", nil},
		{"invalid yaml schema", "../../test/schemas/bad-formatted-yaml.yml", errors.New("yaml: line 27: mapping values are not allowed in this context")},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			uploadSchema, err := ParseUploadSchemasFile(tt.schemaPath)
			assert.Equal(t, tt.expectedError, err)
			log.Println(uploadSchema)
		})
	}
}

func TestValidateSchemas_UploadType(t *testing.T) {
	tests := []struct {
		name          string
		appName       string
		schemas       UploadArtifactSchemas
		expectedError error
	}{
		{name: "valid apt", appName: "some-app-name", schemas: UploadArtifactSchemas{{Src: "some-app-name_0.0.1_amd64.deb", Uploads: []Upload{{Type: TypeApt}}}}, expectedError: nil},
		{name: "valid yum", appName: "some-app-name", schemas: UploadArtifactSchemas{{Src: "some-app-name_0.0.1_amd64.rpm", Uploads: []Upload{{Type: TypeYum}}}}, expectedError: nil},
		{name: "valid file", appName: "some-app-name", schemas: UploadArtifactSchemas{{Src: "some-app-name_0.0.1_amd64.tar-gz", Uploads: []Upload{{Type: TypeFile}}}}, expectedError: nil},
		{name: "valid zypp", appName: "some-app-name", schemas: UploadArtifactSchemas{{Src: "some-app-name_0.0.1_amd64.rpm", Uploads: []Upload{{Type: TypeZypp}}}}, expectedError: nil},
		{name: "invalid type even with valid file", appName: "some-app-name", schemas: UploadArtifactSchemas{{Src: "some-app-name_0.0.1_amd64.deb", Uploads: []Upload{{Type: "something wrong"}}}}, expectedError: errors.New("invalid upload type: something wrong")},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := ValidateSchemas(tt.appName, tt.schemas)
			assert.Equal(t, tt.expectedError, err)
		})
	}
}

func TestValidateSchemas_AppNameShouldBePkgPrefix(t *testing.T) {
	tests := []struct {
		name          string
		appName       string
		schemas       UploadArtifactSchemas
		expectedError error
	}{
		{name: "valid app name with pkg suffix", appName: "some-app-name", schemas: UploadArtifactSchemas{{Src: "some-app-name_some_suffix_0.0.1_amd64.deb", Uploads: []Upload{{Type: TypeApt}}}}, expectedError: nil},
		{name: "valid app name exactly as pkg name", appName: "some-app-name", schemas: UploadArtifactSchemas{{Src: "some-app-name_0.0.1_amd64.deb", Uploads: []Upload{{Type: TypeApt}}}}, expectedError: nil},
		{name: "invalid app name", appName: "some-app-name", schemas: UploadArtifactSchemas{{Src: "some-other-name_0.0.1_amd64.deb", Uploads: []Upload{{Type: TypeFile}}}}, expectedError: errors.New("invalid app name: some-app-name")},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := ValidateSchemas(tt.appName, tt.schemas)
			assert.Equal(t, tt.expectedError, err)
		})
	}
}
