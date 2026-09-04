package loading

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"slices"

	"gopkg.in/yaml.v3"
)

// YamlLoader implements the Loader interface for YAML files.
type YamlLoader struct{}

var yamlBuildFileNames = []string{"BUILD.yaml", "BUILD.yml"}

func (YamlLoader) Matches(fileName string) bool {
	return slices.Contains(yamlBuildFileNames, fileName)
}

// Load reads the file at the specified filePath and unmarshals its content into a model.Package.
func (YamlLoader) Load(_ context.Context, filePath string) (PackageDTO, bool, error) {
	source, operationError := os.ReadFile(filePath)
	if operationError != nil {
		return PackageDTO{}, false, operationError
	}
	packageDTO, operationError := DecodeYAML(source)
	if operationError != nil {
		return PackageDTO{}, true, fmt.Errorf("failed to decode YAML file %s: %w", filePath, operationError)
	}
	return packageDTO, true, nil
}

// DecodeYAML decodes a package from YAML source.
func DecodeYAML(source []byte) (PackageDTO, error) {
	var packageDTO PackageDTO
	if operationError := yaml.NewDecoder(bytes.NewReader(source)).Decode(&packageDTO); operationError != nil {
		return PackageDTO{}, operationError
	}
	return packageDTO, nil
}
