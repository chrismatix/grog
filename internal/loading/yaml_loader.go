package loading

import (
	"bytes"
	"context"
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// YamlLoader implements the Loader interface for YAML files.
type YamlLoader struct{}

func (YamlLoader) Matches(fileName string) bool {
	return IsYAMLPackageFile(fileName)
}

// Load reads the file at the specified filePath and unmarshals its content into a model.Package.
func (loader YamlLoader) Load(ctx context.Context, filePath string) (PackageDTO, bool, error) {
	source, operationError := os.ReadFile(filePath)
	if operationError != nil {
		return PackageDTO{}, false, operationError
	}
	return loader.loadSource(ctx, filePath, source)
}

func (YamlLoader) loadSource(_ context.Context, filePath string, source []byte) (PackageDTO, bool, error) {
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
