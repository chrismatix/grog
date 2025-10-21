package loading

import (
	"context"
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// YamlLoader implements the Loader interface for JSON files.
type YamlLoader struct{}

func (j YamlLoader) Matches(fileName string) bool {
	return "BUILD.yaml" == fileName || "BUILD.yml" == fileName
}

// Load reads the file at the specified filePath and unmarshals its content into a model.Package.
func (j YamlLoader) Load(_ context.Context, filePath string) (PackageDTO, bool, error) {
	var pkg PackageDTO

	// Open the file.
	file, err := os.Open(filePath)
	if err != nil {
		return pkg, false, err
	}
	defer file.Close()

	// Decode JSON content.
	decoder := yaml.NewDecoder(file)
	err = decoder.Decode(&pkg)
	if err != nil {
		return pkg, true, fmt.Errorf(
			"failed to decode JSON file %s: %w",
			filePath,
			err)
	}

	return pkg, true, nil
}
