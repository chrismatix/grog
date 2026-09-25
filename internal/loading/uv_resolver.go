package loading

import (
	"context"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"

	"github.com/pelletier/go-toml/v2"
)

type uvLock struct {
	Packages []uvLockPackage `toml:"package"`
}

type uvLockPackage struct {
	Name   string `toml:"name"`
	Source struct {
		Editable  string `toml:"editable"`
		Directory string `toml:"directory"`
		Virtual   string `toml:"virtual"`
	} `toml:"source"`
	Dependencies         []uvLockDependency            `toml:"dependencies"`
	OptionalDependencies map[string][]uvLockDependency `toml:"optional-dependencies"`
}

type uvLockDependency struct {
	Name string `toml:"name"`
}

// memberDirectory is the workspace-relative directory of a member, or "" for
// index packages and the root project, which has no directory of its own.
func (lockPackage uvLockPackage) memberDirectory() string {
	for _, directory := range []string{lockPackage.Source.Editable, lockPackage.Source.Directory, lockPackage.Source.Virtual} {
		if directory != "" && directory != "." {
			return path.Clean(filepath.ToSlash(directory))
		}
	}
	return ""
}

type uvPyproject struct {
	Project struct {
		Name string `toml:"name"`
	} `toml:"project"`
	Tool struct {
		Uv struct {
			BuildBackend struct {
				ModuleName any     `toml:"module-name"`
				ModuleRoot *string `toml:"module-root"`
			} `toml:"build-backend"`
		} `toml:"uv"`
	} `toml:"tool"`
}

func uvDependencies(resolverContext context.Context, workspaceDirectory string) (resolverDocument, error) {
	document := resolverDocument{Version: 1, Packages: make(map[string]resolverPackage)}
	lock, operationError := readUvLock(workspaceDirectory)
	if operationError != nil {
		return document, operationError
	}
	directoryByName := make(map[string]string)
	for _, lockPackage := range lock.Packages {
		if directory := lockPackage.memberDirectory(); directory != "" {
			directoryByName[lockPackage.Name] = directory
		}
	}
	for _, lockPackage := range lock.Packages {
		if operationError := resolverContext.Err(); operationError != nil {
			return document, operationError
		}
		directory := lockPackage.memberDirectory()
		if directory == "" {
			continue
		}
		// Dev groups are left out: uv permits member cycles, and a test-only
		// edge back to a dependant is the common way to get one.
		dependencies := slices.Clone(lockPackage.Dependencies)
		for _, optional := range lockPackage.OptionalDependencies {
			dependencies = append(dependencies, optional...)
		}
		reportedPackage := resolverPackage{Dependencies: []string{}, Inputs: uvInputs(filepath.Join(workspaceDirectory, directory))}
		for _, dependency := range dependencies {
			if dependencyDirectory, isMember := directoryByName[dependency.Name]; isMember && dependencyDirectory != directory {
				reportedPackage.Dependencies = append(reportedPackage.Dependencies, dependencyDirectory)
			}
		}
		slices.Sort(reportedPackage.Dependencies)
		reportedPackage.Dependencies = slices.Compact(reportedPackage.Dependencies)
		document.Packages[directory] = reportedPackage
	}
	return document, nil
}

var uvFallbackInputs = []string{"**/*.py", "**/*.pyi", "pyproject.toml"}

// uvInputs is the package's module tree plus its manifest. The module comes
// from [tool.uv.build-backend], else from the conventional src/<name> or
// <name> layout; a package with neither falls back to every Python file.
func uvInputs(memberDirectory string) []string {
	inputs := []string{"pyproject.toml"}
	contents, operationError := os.ReadFile(filepath.Join(memberDirectory, "pyproject.toml"))
	var pyproject uvPyproject
	if operationError == nil {
		operationError = toml.Unmarshal(contents, &pyproject)
	}
	if operationError != nil {
		return uvFallbackInputs
	}
	normalizedName := strings.NewReplacer("-", "_", ".", "_").Replace(strings.ToLower(pyproject.Project.Name))
	var moduleDirectories []string
	backend := pyproject.Tool.Uv.BuildBackend
	if backend.ModuleName != nil || backend.ModuleRoot != nil {
		moduleRoot := "src"
		if backend.ModuleRoot != nil {
			moduleRoot = *backend.ModuleRoot
		}
		moduleNames := []string{normalizedName}
		switch names := backend.ModuleName.(type) {
		case string:
			moduleNames = []string{names}
		case []any:
			moduleNames = nil
			for _, name := range names {
				if nameString, isString := name.(string); isString {
					moduleNames = append(moduleNames, nameString)
				}
			}
		}
		for _, moduleName := range moduleNames {
			moduleDirectories = append(moduleDirectories, path.Join(moduleRoot, strings.ReplaceAll(moduleName, ".", "/")))
		}
	} else {
		for _, candidate := range []string{path.Join("src", normalizedName), normalizedName} {
			if info, statError := os.Stat(filepath.Join(memberDirectory, filepath.FromSlash(candidate))); statError == nil && info.IsDir() {
				moduleDirectories = append(moduleDirectories, candidate)
			}
		}
	}
	if len(moduleDirectories) == 0 {
		return uvFallbackInputs
	}
	for _, moduleDirectory := range moduleDirectories {
		inputs = append(inputs, path.Join(moduleDirectory, "**/*"))
	}
	slices.Sort(inputs)
	return slices.Compact(inputs)
}

// uvDefaultInputs are the files the built-in reads: the lock, the root
// manifest, and every member's manifest, which decides its module layout.
func uvDefaultInputs(workspaceDirectory string) []string {
	inputs := []string{"uv.lock", "pyproject.toml"}
	lock, operationError := readUvLock(workspaceDirectory)
	if operationError != nil {
		return inputs
	}
	for _, lockPackage := range lock.Packages {
		if directory := lockPackage.memberDirectory(); directory != "" {
			inputs = append(inputs, path.Join(directory, "pyproject.toml"))
		}
	}
	slices.Sort(inputs)
	return slices.Compact(inputs)
}

func readUvLock(workspaceDirectory string) (uvLock, error) {
	var lock uvLock
	lockPath := filepath.Join(workspaceDirectory, "uv.lock")
	contents, operationError := os.ReadFile(lockPath)
	if operationError != nil {
		return lock, fmt.Errorf("read uv lock %s: %w", lockPath, operationError)
	}
	if operationError := toml.Unmarshal(contents, &lock); operationError != nil {
		return lock, fmt.Errorf("parse uv lock %s: %w", lockPath, operationError)
	}
	return lock, nil
}
