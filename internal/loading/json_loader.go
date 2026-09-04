package loading

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
)

// JsonLoader implements the Loader interface for JSON files.
type JsonLoader struct{}

func (JsonLoader) Matches(fileName string) bool {
	return isPackageFileFormat(fileName, jsonPackageFile)
}

// Load reads the file at the specified filePath and unmarshals its content into a model.Package.
func (loader JsonLoader) Load(ctx context.Context, filePath string) (PackageDTO, bool, error) {
	source, operationError := os.ReadFile(filePath)
	if operationError != nil {
		return PackageDTO{}, false, operationError
	}
	return loader.loadSource(ctx, filePath, source)
}

func (JsonLoader) loadSource(_ context.Context, filePath string, source []byte) (PackageDTO, bool, error) {
	var packageDTO PackageDTO
	if operationError := json.NewDecoder(bytes.NewReader(source)).Decode(&packageDTO); operationError != nil {
		return packageDTO, true, fmt.Errorf("failed to decode JSON file %s: %w", filePath, operationError)
	}
	return packageDTO, true, nil
}
