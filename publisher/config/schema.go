package config

import (
	"fmt"
	"github.com/newrelic/infrastructure-publish-action/publisher/utils"
	"gopkg.in/yaml.v2"
)

//Errors
const (
	noDestinationError = "no uploads were provided for the schema"
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

func ParseUploadSchemasFile(cfgPath string) (UploadArtifactSchemas, error) {

	uploadSchemaContent, err := utils.ReadFileContent(cfgPath)
	if err != nil {
		return nil, err
	}

	uploadSchemas, err := ParseUploadSchema(uploadSchemaContent)
	if err != nil {
		return nil, err
	}

	return uploadSchemas, nil
}

func ParseUploadSchema(fileContent []byte) (UploadArtifactSchemas, error) {

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
