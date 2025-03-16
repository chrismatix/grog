package loading

import (
	"encoding/json"
	"fmt"
	"grog/pkg/config"
	"grog/pkg/model"
	"os"
)

// JSONLoader implements the Loader interface for JSON files.
type JSONLoader struct{}

// FileNames returns the supported JSON file extensions.
func (j JSONLoader) FileNames() []string {
	return []string{"BUILD.json"}
}

// Load reads the file at the specified filePath and unmarshals its content into a model.Package.
func (j JSONLoader) Load(filePath string) (model.Package, error) {
	var pkg model.Package

	// Open the file.
	file, err := os.Open(filePath)
	if err != nil {
		return pkg, err
	}
	defer file.Close()

	// Decode JSON content.
	decoder := json.NewDecoder(file)
	err = decoder.Decode(&pkg)
	if err != nil {
		relPath, workspaceErr := config.GetPathRelativeToWorkspaceRoot(
			filePath)
		if workspaceErr == nil {
			return pkg, fmt.Errorf(
				"failed to decode JSON file %s: %w",
				relPath,
				err)
		}

		return pkg, fmt.Errorf(
			"failed to decode JSON file %s: %w",
			filePath,
			err)
	}

	return pkg, nil
}
