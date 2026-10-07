package hashing

import (
	"fmt"

	"grog/internal/config"
	"grog/internal/model"
)

// GetEnvironmentIdentity hashes everything that decides how an environment
// runs a command: its definition, the provider's input files and the output
// hashes of its dependencies.
func GetEnvironmentIdentity(environment model.Environment, dependencyHashes []string) (string, error) {
	inputHash, err := HashFiles(config.GetPathAbsoluteToWorkspaceRoot(environment.Label.Package), environment.Inputs)
	if err != nil {
		return "", fmt.Errorf("failed hashing inputs for environment %s: %w", environment.Label, err)
	}

	hasher := GetHasher()
	writeIdentityValue(hasher, environment.Label.String())
	writeIdentityValue(hasher, environment.Provider)
	writeIdentityValue(hasher, sortedKeyValue(environment.Config))
	writeIdentityValue(hasher, sortedKeyValue(environment.Fingerprint))
	writeIdentityValue(hasher, inputHash)
	writeIdentityValue(hasher, sorted(dependencyHashes))
	return hasher.SumString(), nil
}
