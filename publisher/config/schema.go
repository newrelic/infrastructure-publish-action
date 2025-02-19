package config

import (
	"errors"
	"fmt"
	"github.com/newrelic/infrastructure-publish-action/publisher/utils"
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

// Define specific error types
var (
	ErrInvalidAppName = errors.New("invalid app name")
	ErrInvalidType    = errors.New("invalid upload type")
)

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
		if err := validateName(appName, schema.Src); err != nil {
			return fmt.Errorf("invalid app name %s for schema %s: %w", appName, schema.Src, err)
		}
		for _, upload := range schema.Uploads {
			if err := validateType(upload.Type); err != nil {
				return fmt.Errorf("invalid uploadType %s for schema %s err: %w", upload.Type, schema.Src, err)
			}
		}
	}
	return nil
}

// validateType checks if the upload type is in the list of valid types fileTypes
func validateType(uploadType string) error {
	for _, validType := range fileTypes {
		if uploadType == validType {
			return nil
		}
	}
	return fmt.Errorf("%w: '%s' (valid types: %s)", ErrInvalidType, uploadType, strings.Join(fileTypes, ", "))
}

func validateName(appName string, src string) error {
	if appName == "" {
		return fmt.Errorf("%w: appName cannot be empty", ErrInvalidAppName)
	}
	if strings.HasPrefix(src, utils.PlaceholderForAppName) {
		return nil
	}
	if !strings.HasPrefix(src, appName) {
		return fmt.Errorf("%w: %s should prefix %s", ErrInvalidAppName, appName, src)
	}
	return nil
}
