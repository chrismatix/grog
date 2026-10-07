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

// nodePackageManager names the files one package manager keeps its
// workspace in. Without a workspaceFile the member globs come from
// "workspaces" in package.json.
type nodePackageManager struct {
	workspaceFile string
	lockfile      string
}

var (
	npmPackageManager  = nodePackageManager{}
	pnpmPackageManager = nodePackageManager{workspaceFile: "pnpm-workspace.yaml", lockfile: "pnpm-lock.yaml"}
	// aube writes its workspace and lockfile in pnpm's v9 format.
	aubePackageManager = nodePackageManager{workspaceFile: "aube-workspace.yaml", lockfile: "aube-lock.yaml"}
)

// dependencies maps the root and every workspace member to the members its
// dependencies, devDependencies, peerDependencies and optionalDependencies
// point at, plus the members the lockfile links it to.
func (packageManager nodePackageManager) dependencies(resolverContext context.Context, workspaceDirectory string) (resolverDocument, error) {
	document := resolverDocument{Version: 1, Packages: make(map[string]resolverPackage)}
	directories, operationError := packageManager.memberDirectories(workspaceDirectory)
	if operationError != nil {
		return document, operationError
	}
	directories = append(directories, "")
	linkedDirectories, operationError := packageManager.linkedDirectories(workspaceDirectory)
	if operationError != nil {
		return document, operationError
	}

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
		reportedPackage := resolverPackage{Dependencies: []string{}, Inputs: []string{"**/*"}, ExcludeInputs: nodeExcludeInputs}
		if directory == "" {
			// Everything else under the root belongs to a member or to another tool.
			reportedPackage.Inputs = []string{"package.json"}
		}
		dependencyDirectories := slices.Clone(linkedDirectories[directory])
		for _, dependencies := range []map[string]string{manifest.Dependencies, manifest.DevDependencies, manifest.PeerDependencies, manifest.OptionalDependencies} {
			for name, specifier := range dependencies {
				if dependencyDirectory, isNamed := directoryByName[nodeDependencyName(name, specifier)]; isNamed {
					dependencyDirectories = append(dependencyDirectories, dependencyDirectory)
				}
				for _, protocol := range []string{"link:", "file:"} {
					if linkPath, isLink := strings.CutPrefix(specifier, protocol); isLink {
						dependencyDirectories = append(dependencyDirectories, path.Join(directory, linkPath))
					}
				}
			}
		}
		for _, dependencyDirectory := range dependencyDirectories {
			if _, isMember := manifests[dependencyDirectory]; isMember && dependencyDirectory != directory {
				reportedPackage.Dependencies = append(reportedPackage.Dependencies, dependencyDirectory)
			}
		}
		slices.Sort(reportedPackage.Dependencies)
		reportedPackage.Dependencies = slices.Compact(reportedPackage.Dependencies)
		document.Packages[directory] = reportedPackage
	}
	return document, nil
}

// nodeDependencyName is the package a dependency installs: the aliased name
// for "workspace:<name>@<range>" and "npm:<name>@<range>", else its key.
func nodeDependencyName(key string, specifier string) string {
	if alias, isAlias := strings.CutPrefix(specifier, "npm:"); isAlias {
		if separator := strings.LastIndex(alias, "@"); separator > 0 {
			return alias[:separator]
		}
		return alias
	}
	if alias, isAlias := strings.CutPrefix(specifier, "workspace:"); isAlias {
		if separator := strings.LastIndex(alias, "@"); separator > 0 {
			return alias[:separator]
		}
	}
	return key
}

// linkedDirectories reads the lockfile's importers and maps each to the
// directories its dependencies resolve to as "link:<path>", which catches
// aliases and overrides. Nothing is linked before the first install.
func (packageManager nodePackageManager) linkedDirectories(workspaceDirectory string) (map[string][]string, error) {
	if packageManager.lockfile == "" {
		return nil, nil
	}
	lockfilePath := filepath.Join(workspaceDirectory, packageManager.lockfile)
	contents, operationError := os.ReadFile(lockfilePath)
	if errors.Is(operationError, os.ErrNotExist) {
		return nil, nil
	}
	if operationError != nil {
		return nil, operationError
	}
	type lockedDependencies map[string]struct {
		Version string `yaml:"version"`
	}
	var lockfile struct {
		Importers map[string]struct {
			Dependencies         lockedDependencies `yaml:"dependencies"`
			DevDependencies      lockedDependencies `yaml:"devDependencies"`
			PeerDependencies     lockedDependencies `yaml:"peerDependencies"`
			OptionalDependencies lockedDependencies `yaml:"optionalDependencies"`
		} `yaml:"importers"`
	}
	if operationError := yaml.Unmarshal(contents, &lockfile); operationError != nil {
		return nil, fmt.Errorf("parse %s: %w", lockfilePath, operationError)
	}
	linkedDirectories := make(map[string][]string, len(lockfile.Importers))
	for importer, importerDependencies := range lockfile.Importers {
		directory := path.Clean(importer)
		if directory == "." {
			directory = ""
		}
		for _, dependencies := range []lockedDependencies{importerDependencies.Dependencies, importerDependencies.DevDependencies, importerDependencies.PeerDependencies, importerDependencies.OptionalDependencies} {
			for _, dependency := range dependencies {
				if linkPath, isLink := strings.CutPrefix(dependency.Version, "link:"); isLink {
					linkedDirectories[directory] = append(linkedDirectories[directory], path.Join(directory, linkPath))
				}
			}
		}
	}
	return linkedDirectories, nil
}

// memberDirectories resolves the workspace's member globs to the
// directories holding a package.json. A "!" pattern removes its matches.
func (packageManager nodePackageManager) memberDirectories(workspaceDirectory string) ([]string, error) {
	patterns, operationError := packageManager.memberPatterns(workspaceDirectory)
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

// memberPatterns reads the member globs from the package manager's
// workspace file, else from "workspaces" in package.json, which npm writes
// as a list and yarn may wrap in an object.
func (packageManager nodePackageManager) memberPatterns(workspaceDirectory string) ([]string, error) {
	if packageManager.workspaceFile != "" {
		workspacePath := filepath.Join(workspaceDirectory, packageManager.workspaceFile)
		contents, operationError := os.ReadFile(workspacePath)
		if errors.Is(operationError, os.ErrNotExist) {
			return nil, fmt.Errorf("no %s in %s", packageManager.workspaceFile, workspaceDirectory)
		}
		if operationError != nil {
			return nil, operationError
		}
		var workspace struct {
			Packages []string `yaml:"packages"`
		}
		if operationError := yaml.Unmarshal(contents, &workspace); operationError != nil {
			return nil, fmt.Errorf("parse %s: %w", workspacePath, operationError)
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
		return nil, fmt.Errorf("no workspaces in %s", filepath.Join(workspaceDirectory, "package.json"))
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

// nodeExcludeInputs keeps every glob out of installed packages.
var nodeExcludeInputs = []string{"**/node_modules/**"}

// defaultInputs are the files the built-in reads: package.json, the package
// manager's workspace file and lockfile, and a package.json under every
// member glob, so a new member is seen without a re-declaration. Negated
// member globs become exclusions.
func (packageManager nodePackageManager) defaultInputs(workspaceDirectory string) (inputs []string, excludeInputs []string) {
	inputs = []string{"package.json"}
	for _, fileName := range []string{packageManager.workspaceFile, packageManager.lockfile} {
		if fileName != "" {
			inputs = append(inputs, fileName)
		}
	}
	excludeInputs = nodeExcludeInputs
	patterns, operationError := packageManager.memberPatterns(workspaceDirectory)
	if operationError != nil {
		return inputs, excludeInputs
	}
	for _, pattern := range patterns {
		if negated := strings.HasPrefix(pattern, "!"); negated {
			excludeInputs = append(excludeInputs, strings.TrimSuffix(strings.TrimPrefix(pattern, "!"), "/**")+"/**")
		} else {
			inputs = append(inputs, path.Join(pattern, "package.json"))
		}
	}
	return inputs, excludeInputs
}
