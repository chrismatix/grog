package loading

import (
	"context"
	"fmt"
	"os"
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
		Members []string `toml:"members"`
	} `toml:"workspace"`
	Dependencies      map[string]any                   `toml:"dependencies"`
	BuildDependencies map[string]any                   `toml:"build-dependencies"`
	Target            map[string]cargoDependencyTables `toml:"target"`
}

func cargoDependencies(providerContext context.Context, workspaceDirectory string) (providerDocument, error) {
	document := providerDocument{Version: 1, Packages: make(map[string]providerPackage)}
	workspaceManifest, operationError := readCargoManifest(providerContext, filepath.Join(workspaceDirectory, "Cargo.toml"))
	if operationError != nil {
		return document, operationError
	}
	members := make(map[string]string)
	for _, pattern := range workspaceManifest.Workspace.Members {
		matches, operationError := filepath.Glob(filepath.Join(workspaceDirectory, pattern))
		if operationError != nil {
			return document, fmt.Errorf("invalid cargo workspace member glob %q: %w", pattern, operationError)
		}
		for _, memberDirectory := range matches {
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
		manifest, operationError := readCargoManifest(providerContext, filepath.Join(memberDirectory, "Cargo.toml"))
		if operationError != nil {
			return document, operationError
		}
		reportedPackage := providerPackage{Dependencies: []string{}}
		dependencyTables := []map[string]any{manifest.Dependencies, manifest.BuildDependencies}
		for _, conditional := range manifest.Target {
			dependencyTables = append(dependencyTables, conditional.Dependencies, conditional.BuildDependencies)
		}
		for _, dependencies := range dependencyTables {
			for _, dependency := range dependencies {
				declaration, isTable := dependency.(map[string]any)
				if !isTable {
					continue
				}
				dependencyPath, hasPath := declaration["path"].(string)
				if !hasPath {
					continue
				}
				dependencyDirectory := filepath.Clean(filepath.Join(memberDirectory, dependencyPath))
				if dependencyMember, isMember := members[dependencyDirectory]; isMember && dependencyMember != memberPath {
					reportedPackage.Dependencies = append(reportedPackage.Dependencies, dependencyMember)
				}
			}
		}
		slices.Sort(reportedPackage.Dependencies)
		reportedPackage.Dependencies = slices.Compact(reportedPackage.Dependencies)
		document.Packages[memberPath] = reportedPackage
	}
	return document, providerContext.Err()
}

func readCargoManifest(providerContext context.Context, manifestPath string) (cargoManifest, error) {
	var manifest cargoManifest
	if operationError := providerContext.Err(); operationError != nil {
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
