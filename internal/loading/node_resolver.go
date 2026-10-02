package loading

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"

	"github.com/bmatcuk/doublestar/v4"
	"gopkg.in/yaml.v3"
)

type nodeManifest struct {
	Name                 string            `json:"name"`
	Workspaces           json.RawMessage   `json:"workspaces"`
	Dependencies         map[string]string `json:"dependencies"`
	DevDependencies      map[string]string `json:"devDependencies"`
	PeerDependencies     map[string]string `json:"peerDependencies"`
	OptionalDependencies map[string]string `json:"optionalDependencies"`
}

// nodeDependencies maps the root and every workspace member to the members
// named in its dependencies, devDependencies, peerDependencies and
// optionalDependencies.
func nodeDependencies(resolverContext context.Context, workspaceDirectory string) (resolverDocument, error) {
	document := resolverDocument{Version: 1, Packages: make(map[string]resolverPackage)}
	directories, operationError := nodeMemberDirectories(workspaceDirectory)
	if operationError != nil {
		return document, operationError
	}
	directories = append(directories, "")

	manifests := make(map[string]nodeManifest, len(directories))
	directoryByName := make(map[string]string, len(directories))
	for _, directory := range directories {
		if operationError := resolverContext.Err(); operationError != nil {
			return document, operationError
		}
		manifest, operationError := readNodeManifest(filepath.Join(workspaceDirectory, directory, "package.json"))
		if operationError != nil {
			return document, operationError
		}
		manifests[directory] = manifest
		if manifest.Name != "" {
			directoryByName[manifest.Name] = directory
		}
	}

	for directory, manifest := range manifests {
		reportedPackage := resolverPackage{Dependencies: []string{}, Inputs: nodeInputs(filepath.Join(workspaceDirectory, directory))}
		if directory == "" {
			// Everything else under the root belongs to a member or to another tool.
			reportedPackage.Inputs = []string{"package.json"}
		}
		for _, dependencies := range []map[string]string{manifest.Dependencies, manifest.DevDependencies, manifest.PeerDependencies, manifest.OptionalDependencies} {
			for name := range dependencies {
				if dependencyDirectory, isMember := directoryByName[name]; isMember && dependencyDirectory != directory {
					reportedPackage.Dependencies = append(reportedPackage.Dependencies, dependencyDirectory)
				}
			}
		}
		slices.Sort(reportedPackage.Dependencies)
		reportedPackage.Dependencies = slices.Compact(reportedPackage.Dependencies)
		document.Packages[directory] = reportedPackage
	}
	return document, nil
}

// nodeMemberDirectories resolves the workspace's member globs to the
// directories holding a package.json. A "!" pattern removes its matches.
func nodeMemberDirectories(workspaceDirectory string) ([]string, error) {
	patterns, operationError := nodeMemberPatterns(workspaceDirectory)
	if operationError != nil {
		return nil, operationError
	}
	fileSystem := os.DirFS(workspaceDirectory)
	var directories, excluded []string
	for _, pattern := range patterns {
		negated := strings.HasPrefix(pattern, "!")
		pattern = strings.TrimSuffix(strings.TrimPrefix(pattern, "!"), "/")
		matches, operationError := doublestar.Glob(fileSystem, path.Join(pattern, "package.json"))
		if operationError != nil {
			return nil, fmt.Errorf("resolve workspace pattern %q: %w", pattern, operationError)
		}
		for _, match := range matches {
			directory := path.Dir(match)
			if directory == "." || slices.Contains(strings.Split(directory, "/"), "node_modules") {
				continue
			}
			if negated {
				excluded = append(excluded, directory)
			} else {
				directories = append(directories, directory)
			}
		}
	}
	directories = slices.DeleteFunc(directories, func(directory string) bool { return slices.Contains(excluded, directory) })
	slices.Sort(directories)
	return slices.Compact(directories), nil
}

// nodeMemberPatterns reads the member globs from pnpm-workspace.yaml or
// aube-workspace.yaml, which share a format, else from "workspaces" in
// package.json, which npm writes as a list and yarn may wrap in an object.
func nodeMemberPatterns(workspaceDirectory string) ([]string, error) {
	for _, fileName := range []string{"pnpm-workspace.yaml", "aube-workspace.yaml"} {
		contents, operationError := os.ReadFile(filepath.Join(workspaceDirectory, fileName))
		if errors.Is(operationError, os.ErrNotExist) {
			continue
		}
		if operationError != nil {
			return nil, operationError
		}
		var workspace struct {
			Packages []string `yaml:"packages"`
		}
		if operationError := yaml.Unmarshal(contents, &workspace); operationError != nil {
			return nil, fmt.Errorf("parse %s: %w", filepath.Join(workspaceDirectory, fileName), operationError)
		}
		return workspace.Packages, nil
	}

	manifest, operationError := readNodeManifest(filepath.Join(workspaceDirectory, "package.json"))
	if operationError != nil {
		return nil, operationError
	}
	var patterns []string
	if len(manifest.Workspaces) > 0 && json.Unmarshal(manifest.Workspaces, &patterns) != nil {
		var workspaces struct {
			Packages []string `json:"packages"`
		}
		if operationError := json.Unmarshal(manifest.Workspaces, &workspaces); operationError != nil {
			return nil, fmt.Errorf("parse workspaces in %s: %w", filepath.Join(workspaceDirectory, "package.json"), operationError)
		}
		patterns = workspaces.Packages
	}
	if len(patterns) == 0 {
		return nil, fmt.Errorf("no workspace in %s: expected pnpm-workspace.yaml, aube-workspace.yaml or workspaces in package.json", workspaceDirectory)
	}
	return patterns, nil
}

func readNodeManifest(manifestPath string) (nodeManifest, error) {
	var manifest nodeManifest
	contents, operationError := os.ReadFile(manifestPath)
	if operationError != nil {
		return manifest, fmt.Errorf("read %s: %w", manifestPath, operationError)
	}
	if operationError := json.Unmarshal(contents, &manifest); operationError != nil {
		return manifest, fmt.Errorf("parse %s: %w", manifestPath, operationError)
	}
	return manifest, nil
}

// nodeInputs is every top-level file and directory of the member except
// node_modules, which no package manager keeps small or stable.
func nodeInputs(memberDirectory string) []string {
	entries, operationError := os.ReadDir(memberDirectory)
	if operationError != nil {
		return []string{"package.json"}
	}
	inputs := make([]string, 0, len(entries))
	for _, entry := range entries {
		switch {
		case entry.Name() == "node_modules":
		case entry.IsDir():
			inputs = append(inputs, entry.Name()+"/**/*")
		default:
			inputs = append(inputs, entry.Name())
		}
	}
	slices.Sort(inputs)
	return inputs
}

// nodeDefaultInputs are the files the built-in reads plus the lockfiles,
// which change when a member is added under an existing glob.
func nodeDefaultInputs(workspaceDirectory string) []string {
	inputs := []string{"package.json", "pnpm-workspace.yaml", "aube-workspace.yaml", "pnpm-lock.yaml", "package-lock.json", "yarn.lock"}
	directories, operationError := nodeMemberDirectories(workspaceDirectory)
	if operationError != nil {
		return inputs
	}
	for _, directory := range directories {
		inputs = append(inputs, path.Join(directory, "package.json"))
	}
	return inputs
}
