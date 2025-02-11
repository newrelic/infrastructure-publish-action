package config

import (
	"fmt"
	"gopkg.in/yaml.v2"
	"io/ioutil"
	"strings"
)

// Errors
const (
	noDestinationError = "no uploads were provided for the schema"
	//FileTypes
	TypeFile = "file"
	TypeZypp = "zypp"
	TypeYum  = "yum"
	TypeApt  = "apt"
)

var fileTypes = []string{TypeFile, TypeZypp, TypeYum, TypeApt}

type UploadArtifactSchema struct {
	Src     string   `yaml:"src"`
	Arch    []string `yaml:"arch"`
	Uploads []Upload `yaml:"uploads"`
}

type Upload struct {
	Type      string   `yaml:"type"` // verify type in allowed list file, apt, yum, zypp
	SrcRepo   string   `yaml:"src_repo"`
	Dest      string   `yaml:"dest"`
	Override  bool     `yaml:"override"`
	OsVersion []string `yaml:"os_version"`
}

type UploadArtifactSchemas []UploadArtifactSchema

// ParseUploadSchemasFile reads content of a file and marshal it into yaml
// config struct
func ParseUploadSchemasFile(cfgPath string) (UploadArtifactSchemas, error) {

	uploadSchemaContent, err := ioutil.ReadFile(cfgPath)
	if err != nil {
		return nil, err
	}

	uploadSchemas, err := parseUploadSchema(uploadSchemaContent)
	if err != nil {
		return nil, err
	}

	return uploadSchemas, nil
}

func parseUploadSchema(fileContent []byte) (UploadArtifactSchemas, error) {

	var schema UploadArtifactSchemas

	err := yaml.Unmarshal(fileContent, &schema)

	if err != nil {
		return nil, err
	}

	for i := range schema {
		if schema[i].Arch == nil {
			schema[i].Arch = []string{""}
		}
		if len(schema[i].Uploads) == 0 {
			return nil, fmt.Errorf("error: '%s' in the schema: %v ", noDestinationError, schema[i].Src)
		}
	}

	return schema, nil
}

// ValidateSchemas validates that some components of the schema are correct (this is still wip)
// It validates:
//   - no incorrect fileType is present
//   - the app name is the prefix of the src package. This is mandatory to create consistent apt repositories.
//     If they are not equal, the file will be uploaded to a location, and the metadata will point to different location
//     which will break the apt repository as you'll receive a 404 when trying to install the package.
func ValidateSchemas(appName string, schemas UploadArtifactSchemas) error {
	for _, schema := range schemas {
		if !isValidAppName(appName, schema) {
			return fmt.Errorf("invalid app name: %s", appName)
		}
		for _, upload := range schema.Uploads {
			if !isValidType(upload.Type) {
				return fmt.Errorf("invalid upload type: %s", upload.Type)
			}
		}
	}
	return nil
}

func isValidType(uploadType string) bool {
	for _, validType := range fileTypes {
		if uploadType == validType {
			return true
		}
	}
	return false
}

func isValidAppName(appName string, schemas UploadArtifactSchema) bool {
	return strings.HasPrefix(schemas.Src, appName)
}
