package hashing

import (
	"fmt"
	"github.com/cespare/xxhash/v2"
	"grog/internal/config"
	"grog/internal/model"
	"slices"
	"strings"
)

// GetTargetChangeHash computes the hash that tells us if a target has changed.
// dependencyHashes are the change hashes of the direct dependencies
func GetTargetChangeHash(target model.Target, dependencyHashes []string) (string, error) {
	targetDefinitionHash, err := hashTargetDefinition(target, dependencyHashes)
	if err != nil {
		return "", err
	}
	absolutePackagePath := config.GetPathAbsoluteToWorkspaceRoot(target.Label.Package)
	if len(target.Inputs) == 0 {
		return targetDefinitionHash, nil
	}

	inputContentHash, err := HashFiles(absolutePackagePath, target.Inputs)
	return fmt.Sprintf("%s_%s", targetDefinitionHash, inputContentHash), nil
}

// hashTargetDefinition computes the xxhash hash of a single file.
func hashTargetDefinition(target model.Target, dependencyHashes []string) (string, error) {
	hasher := xxhash.New()

	_, err := hasher.WriteString(target.Label.String())
	_, err = hasher.WriteString(target.Command)
	_, err = hasher.WriteString(sorted(target.Inputs))
	_, err = hasher.WriteString(sorted(target.OutputDefinitions()))
	_, err = hasher.WriteString(sorted(dependencyHashes))
	if !target.IsMultiplatformCache() {
		_, err = hasher.WriteString(config.Global.GetPlatform())
	}

	if err != nil {
		return "", err
	}
	// Return the hash as a hexadecimal string.
	return fmt.Sprintf("%x", hasher.Sum64()), nil
}

func sorted(s []string) string {
	slices.Sort(s)
	return strings.Join(s, ",")
}
