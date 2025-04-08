package loading

import (
	"fmt"
	"gopkg.in/yaml.v3"
	"os"
)

// YamlLoader implements the Loader interface for JSON files.
type YamlLoader struct{}

// FileNames returns the supported JSON file extensions.
func (j YamlLoader) FileNames() []string {
	return []string{"BUILD.yaml", "BUILD.yml"}
}

// Load reads the file at the specified filePath and unmarshals its content into a model.Package.
func (j YamlLoader) Load(filePath string) (PackageDTO, error) {
	var pkg PackageDTO

	// Open the file.
	file, err := os.Open(filePath)
	if err != nil {
		return pkg, err
	}
	defer file.Close()

	// Decode JSON content.
	decoder := yaml.NewDecoder(file)
	err = decoder.Decode(&pkg)
	if err != nil {
		return pkg, fmt.Errorf(
			"failed to decode JSON file %s: %w",
			filePath,
			err)
	}

	return pkg, nil
}
