package main

import (
	"fmt"
	"github.com/cespare/xxhash/v2"
	"io"
	"os"
)

// HashFile computes the xxhash hash of a single file.
func HashTarget(filePath string) (string, error) {
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
