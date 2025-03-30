package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"

	"github.com/cespare/xxhash/v2"
)

// HashFile computes the xxhash hash of a single file.
func HashFile(filePath string) (string, error) {
	f, err := os.Open(filePath)
	if err != nil {
		return "", err
	}
	defer f.Close()

	hasher := xxhash.New()
	if _, err := io.Copy(hasher, f); err != nil {
		return "", err
	}
	// Return the hash as a hexadecimal string.
	return fmt.Sprintf("%x", hasher.Sum64()), nil
}

// HashFiles computes a combined xxhash hash for multiple files relative to packagePath
// Sorts the array to ensure consistent outputs.
func HashFiles(packagePath string, fileList []string) (string, error) {
	combinedHasher := xxhash.New()
	// Ensure consistent ordering.
	sort.Strings(fileList)

	for _, file := range fileList {
		// Create the full file path.
		fullPath := filepath.Join(packagePath, file)
		f, err := os.Open(fullPath)
		if err != nil {
			return "", err
		}

		// Copy file content into the combined hasher.
		if _, err := io.Copy(combinedHasher, f); err != nil {
			f.Close()
			return "", err
		}
		f.Close()
	}

	// Return the combined hash as a hexadecimal string.
	return fmt.Sprintf("%x", combinedHasher.Sum64()), nil
}
