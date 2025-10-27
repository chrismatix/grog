package loading

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
)

// JsonLoader implements the Loader interface for JSON files.
type JsonLoader struct{}

func (j JsonLoader) Matches(fileName string) bool {
	return "BUILD.json" == fileName
}

// Load reads the file at the specified filePath and unmarshals its content into a model.Package.
func (j JsonLoader) Load(_ context.Context, filePath string) (PackageDTO, bool, error) {
	var pkg PackageDTO

	// Open the file.
	file, err := os.Open(filePath)
	if err != nil {
		return pkg, false, err
	}
	defer file.Close()

	// Decode JSON content.
	decoder := json.NewDecoder(file)
	err = decoder.Decode(&pkg)
	if err != nil {
		return pkg, true, fmt.Errorf(
			"failed to decode JSON file %s: %w",
			filePath,
			err)
	}

	return pkg, true, nil
}
