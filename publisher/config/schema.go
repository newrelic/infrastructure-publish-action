package config

import (
	"fmt"
	"gopkg.in/yaml.v2"
	"io/ioutil"
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
