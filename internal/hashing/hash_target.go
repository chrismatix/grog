package hashing

import (
	"fmt"
	"grog/internal/config"
	"grog/internal/model"
	"slices"
	"strings"
)

// GetTargetChangeHash computes the hash that tells us if a target has changed.
// dependencyHashes are the change hashes of the direct dependencies.
func GetTargetChangeHash(target model.Target, dependencyHashes []string, extraArgs []string) (string, error) {
	targetDefinitionHash, err := hashTargetDefinition(target, dependencyHashes, extraArgs)
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

// GetTargetChangeHashAtRef computes a target's change hash as its input files
// existed at gitRef, reusing the supplied dependencyHashes for the definition
// component. It reproduces GetTargetChangeHash exactly when the at-ref input
// contents and the dependency hashes match those of a build at that ref, so the
// result identifies the cache image a prior build would have produced.
//
// gitRoot is the repository root (for resolving repo-relative paths). The
// dependency hashes are taken as given rather than recomputed at the ref: a
// drifted dependency only changes which prior image is identified (a missed
// seed, degrading to a cold build), never correctness, because this hash is
// used solely to locate a layer-cache donor and never to restore a result.
func GetTargetChangeHashAtRef(
	target model.Target,
	dependencyHashes []string,
	extraArgs []string,
	gitRoot string,
	gitRef string,
) (string, error) {
	targetDefinitionHash, err := hashTargetDefinition(target, dependencyHashes, extraArgs)
	if err != nil {
		return "", err
	}
	if len(target.Inputs) == 0 {
		return targetDefinitionHash, nil
	}

	absolutePackagePath := config.GetPathAbsoluteToWorkspaceRoot(target.Label.Package)
	inputContentHash, err := HashFilesAtRef(gitRoot, gitRef, absolutePackagePath, target.Inputs)
	if err != nil {
		return "", fmt.Errorf("failed hashing input files %s for target %s at ref %s: %w",
			strings.Join(target.Inputs, ","), target.Label, gitRef, err)
	}
	return fmt.Sprintf("%s_%s", targetDefinitionHash, inputContentHash), nil
}

// hashTargetDefinition computes the configured hash of a single file.
func hashTargetDefinition(target model.Target, dependencyHashes []string, extraArgs []string) (string, error) {
	hasher := GetHasher()

	if _, err := hasher.WriteString(target.Label.String()); err != nil {
		return "", err
	}
	if _, err := hasher.WriteString(target.Command); err != nil {
		return "", err
	}
	if _, err := hasher.WriteString(sorted(target.Inputs)); err != nil {
		return "", err
	}
	if _, err := hasher.WriteString(sorted(target.OutputDefinitions())); err != nil {
		return "", err
	}
	if _, err := hasher.WriteString(sorted(dependencyHashes)); err != nil {
		return "", err
	}
	if _, err := hasher.WriteString(sortedKeyValue(target.Fingerprint)); err != nil {
		return "", err
	}

	// Include extra command-line arguments (e.g. from "grog test -- -k foo")
	// so that different invocations with different flags bust the cache.
	if len(extraArgs) > 0 {
		if _, err := hasher.WriteString(strings.Join(extraArgs, "\x00")); err != nil {
			return "", err
		}
	}

	// By default, target hashes are separate between platforms unless
	// the target has a multiplatform-cache tag
	if !target.IsMultiplatformCache() {
		if _, err := hasher.WriteString(config.Global.GetPlatform()); err != nil {
			return "", err
		}
		if len(config.Global.PlatformTags) > 0 {
			tags := slices.Clone(config.Global.PlatformTags)
			if _, err := hasher.WriteString(sorted(tags)); err != nil {
				return "", err
			}
		}
	}

	// Include the docker backend in the hash for targets with docker outputs
	// so that cache results from different backends (fs vs registry) can co-exist
	if hasDockerOutput(target) {
		if _, err := hasher.WriteString(config.Global.OCI.Backend); err != nil {
			return "", err
		}
	}
	// Return the hash as a hexadecimal string.
	return hasher.SumString(), nil
}

func hasDockerOutput(target model.Target) bool {
	for _, output := range target.AllOutputs() {
		if output.Type == "oci" {
			return true
		}
	}
	return false
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
