package loading

import (
	"context"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"slices"

	"github.com/pelletier/go-toml/v2"
)

type cargoDependencyTables struct {
	Dependencies      map[string]any `toml:"dependencies"`
	BuildDependencies map[string]any `toml:"build-dependencies"`
}

type cargoManifest struct {
	Workspace struct {
		Members      []string       `toml:"members"`
		Exclude      []string       `toml:"exclude"`
		Dependencies map[string]any `toml:"dependencies"`
	} `toml:"workspace"`
	Package struct {
		Name    string   `toml:"name"`
		Include []string `toml:"include"`
	} `toml:"package"`
	Lib struct {
		Path string `toml:"path"`
	} `toml:"lib"`
	Bin []struct {
		Path string `toml:"path"`
	} `toml:"bin"`
	Dependencies      map[string]any                   `toml:"dependencies"`
	BuildDependencies map[string]any                   `toml:"build-dependencies"`
	Target            map[string]cargoDependencyTables `toml:"target"`
}

func cargoDependencies(resolverContext context.Context, workspaceDirectory string) (resolverDocument, error) {
	document := resolverDocument{Version: 1, Packages: make(map[string]resolverPackage)}
	workspaceManifest, operationError := readCargoManifest(resolverContext, filepath.Join(workspaceDirectory, "Cargo.toml"))
	if operationError != nil {
		return document, operationError
	}
	excluded := make(map[string]bool)
	for _, pattern := range workspaceManifest.Workspace.Exclude {
		matches, operationError := filepath.Glob(filepath.Join(workspaceDirectory, pattern))
		if operationError != nil {
			return document, fmt.Errorf("invalid cargo workspace exclude glob %q: %w", pattern, operationError)
		}
		for _, excludedDirectory := range matches {
			excluded[filepath.Clean(excludedDirectory)] = true
		}
	}
	members := make(map[string]string)
	// A root manifest with a [package] table is a member without being listed.
	if workspaceManifest.Package.Name != "" {
		members[filepath.Clean(workspaceDirectory)] = ""
	}
	for _, pattern := range workspaceManifest.Workspace.Members {
		matches, operationError := filepath.Glob(filepath.Join(workspaceDirectory, pattern))
		if operationError != nil {
			return document, fmt.Errorf("invalid cargo workspace member glob %q: %w", pattern, operationError)
		}
		for _, memberDirectory := range matches {
			if excluded[filepath.Clean(memberDirectory)] {
				continue
			}
			relativePath, operationError := filepath.Rel(workspaceDirectory, memberDirectory)
			if operationError != nil {
				return document, fmt.Errorf("resolve cargo workspace member %s: %w", memberDirectory, operationError)
			}
			if relativePath == "." {
				relativePath = ""
			}
			members[filepath.Clean(memberDirectory)] = filepath.ToSlash(relativePath)
		}
	}
	for memberDirectory, memberPath := range members {
		manifest, operationError := readCargoManifest(resolverContext, filepath.Join(memberDirectory, "Cargo.toml"))
		if operationError != nil {
			return document, operationError
		}
		reportedPackage := resolverPackage{Dependencies: []string{}, Inputs: cargoInputs(manifest)}
		dependencyTables := []map[string]any{manifest.Dependencies, manifest.BuildDependencies}
		for _, conditional := range manifest.Target {
			dependencyTables = append(dependencyTables, conditional.Dependencies, conditional.BuildDependencies)
		}
		for _, dependencies := range dependencyTables {
			for dependencyName, dependency := range dependencies {
				dependencyDirectory, isPathDependency := cargoDependencyDirectory(dependencyName, dependency, memberDirectory, workspaceManifest, workspaceDirectory)
				if !isPathDependency {
					continue
				}
				if dependencyMember, isMember := members[dependencyDirectory]; isMember && dependencyMember != memberPath {
					reportedPackage.Dependencies = append(reportedPackage.Dependencies, dependencyMember)
				}
			}
		}
		slices.Sort(reportedPackage.Dependencies)
		reportedPackage.Dependencies = slices.Compact(reportedPackage.Dependencies)
		document.Packages[memberPath] = reportedPackage
	}
	return document, resolverContext.Err()
}

// cargoDependencyDirectory returns the directory a path dependency points at.
// `workspace = true` entries take their path from the root manifest's
// [workspace.dependencies] table, relative to the workspace root.
func cargoDependencyDirectory(dependencyName string, dependency any, memberDirectory string, workspaceManifest cargoManifest, workspaceDirectory string) (string, bool) {
	declaration, isTable := dependency.(map[string]any)
	if !isTable {
		return "", false
	}
	baseDirectory := memberDirectory
	if inherited, isInherited := declaration["workspace"].(bool); isInherited && inherited {
		declaration, isTable = workspaceManifest.Workspace.Dependencies[dependencyName].(map[string]any)
		if !isTable {
			return "", false
		}
		baseDirectory = workspaceDirectory
	}
	dependencyPath, hasPath := declaration["path"].(string)
	if !hasPath {
		return "", false
	}
	return filepath.Clean(filepath.Join(baseDirectory, dependencyPath)), true
}

// cargoDefaultInputs are the files the built-in reads: the root manifest and
// lockfile plus a manifest under every member pattern, however deep it is.
func cargoDefaultInputs(workspaceDirectory string) []string {
	inputs := []string{"Cargo.toml", "Cargo.lock"}
	contents, operationError := os.ReadFile(filepath.Join(workspaceDirectory, "Cargo.toml"))
	if operationError != nil {
		return inputs
	}
	var manifest cargoManifest
	if operationError := toml.Unmarshal(contents, &manifest); operationError != nil {
		return inputs
	}
	for _, pattern := range manifest.Workspace.Members {
		inputs = append(inputs, path.Join(pattern, "Cargo.toml"))
	}
	return inputs
}

// cargoInputs over-approximates on purpose: a listed file the crate lacks is
// skipped when hashing, while one left out would under-invalidate silently.
func cargoInputs(manifest cargoManifest) []string {
	inputs := []string{"Cargo.toml", "build.rs", "src/**/*", "tests/**/*", "benches/**/*", "examples/**/*"}
	if manifest.Lib.Path != "" {
		inputs = append(inputs, manifest.Lib.Path)
	}
	for _, binary := range manifest.Bin {
		if binary.Path != "" {
			inputs = append(inputs, binary.Path)
		}
	}
	inputs = append(inputs, manifest.Package.Include...)
	slices.Sort(inputs)
	return slices.Compact(inputs)
}

func readCargoManifest(resolverContext context.Context, manifestPath string) (cargoManifest, error) {
	var manifest cargoManifest
	if operationError := resolverContext.Err(); operationError != nil {
		return manifest, operationError
	}
	contents, operationError := os.ReadFile(manifestPath)
	if operationError != nil {
		return manifest, fmt.Errorf("read cargo manifest %s: %w", manifestPath, operationError)
	}
	if operationError := toml.Unmarshal(contents, &manifest); operationError != nil {
		return manifest, fmt.Errorf("parse cargo manifest %s: %w", manifestPath, operationError)
	}
	return manifest, nil
}
