package hashing

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
)

// HashFile computes the configured hash of a single file.
func HashFile(filePath string) (string, error) {
	f, err := os.Open(filePath)
	if err != nil {
		return "", err
	}
	defer f.Close()

	hasher := GetHasher()
	if _, err := io.Copy(hasher, f); err != nil {
		return "", err
	}
	// Return the hash as a hexadecimal string.
	return hasher.SumString(), nil
}

// HashFiles computes a combined hash for multiple files relative to packagePath
// Sorts the array to ensure consistent outputs.
func HashFiles(absolutePackagePath string, fileList []string) (string, error) {
	combinedHasher := GetHasher()
	// Ensure consistent ordering.
	sort.Strings(fileList)

	for _, file := range fileList {
		// Create the full file path.
		fullPath := filepath.Join(absolutePackagePath, file)
		f, err := os.Open(fullPath)
		if err != nil {
			if os.IsNotExist(err) {
				// NOTE: If a file does not exist in the package, we skip it.
				// TODO make this a warning
				continue
			}
			return "", fmt.Errorf("failed opening input file for hashing: %w", err)
		}

		// Copy file content into the combined hasher.
		if _, err := io.Copy(combinedHasher, f); err != nil {
			f.Close()
			return "", err
		}
		f.Close()
	}

	// Return the combined hash as a hexadecimal string.
	return combinedHasher.SumString(), nil
}
