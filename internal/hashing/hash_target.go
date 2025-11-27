package hashing

import (
	"fmt"
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
	if err != nil {
		return "", fmt.Errorf("failed hashing input files %s for target %s: %w", strings.Join(target.Inputs, ","), target.Label, err)
	}
	return fmt.Sprintf("%s_%s", targetDefinitionHash, inputContentHash), err
}

// hashTargetDefinition computes the configured hash of a single file.
func hashTargetDefinition(target model.Target, dependencyHashes []string) (string, error) {
	hasher := GetHasher()

	_, err := hasher.WriteString(target.Label.String())
	_, err = hasher.WriteString(target.Command)
	_, err = hasher.WriteString(sorted(target.Inputs))
	_, err = hasher.WriteString(sorted(target.OutputDefinitions()))
	_, err = hasher.WriteString(sorted(dependencyHashes))
	_, err = hasher.WriteString(sortedKeyValue(target.Fingerprint))
	if !target.IsMultiplatformCache() {
		_, err = hasher.WriteString(config.Global.GetPlatform())
	}

	if err != nil {
		return "", err
	}
	// Return the hash as a hexadecimal string.
	return hasher.SumString(), nil
}

func sorted(s []string) string {
	slices.Sort(s)
	return strings.Join(s, ",")
}

func sortedKeyValue(m map[string]string) string {
	if len(m) == 0 {
		return ""
	}

	entries := make([]string, 0, len(m))
	for k, v := range m {
		entries = append(entries, fmt.Sprintf("%s=%s", k, v))
	}

	return sorted(entries)
}
