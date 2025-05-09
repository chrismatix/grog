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
func GetTargetChangeHash(target model.Target) (string, error) {
	targetDefinitionHash, err := hashTargetDefinition(target)
	if err != nil {
		return "", err
	}
	absolutePackagePath := config.GetPathAbsoluteToWorkspaceRoot(target.Label.Package)
	inputContentHash, err := HashFiles(absolutePackagePath, target.Inputs)

	return fmt.Sprintf("%s_%s", targetDefinitionHash, inputContentHash), nil
}

// hashTargetDefinition computes the xxhash hash of a single file.
func hashTargetDefinition(target model.Target) (string, error) {
	hasher := xxhash.New()

	_, err := hasher.WriteString(target.Label.String())
	_, err = hasher.WriteString(target.Command)
	_, err = hasher.WriteString(sorted(target.Inputs))
	_, err = hasher.WriteString(sorted(target.OutputDefinitions()))
	_, err = hasher.WriteString(sorted(target.GetDepsString()))
	_, err = hasher.WriteString(config.Global.GetPlatform())

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
