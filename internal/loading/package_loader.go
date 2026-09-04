package loading

import (
	"context"
	"path/filepath"

	"grog/internal/console"
)

// Loader Implement this to provide a loader for a user provided BUILD file format.
// Load must be safe for concurrent use by multiple goroutines.
type Loader interface {
	// Matches indicates if the loader can load the specified file name
	Matches(fileName string) bool
	// Load reads the file at the specified filePath and unmarshals its content into a model.Package
	// Returns true if the file contains a valid package definition (needed for Makefiles)
	Load(ctx context.Context, filePath string) (PackageDTO, bool, error)
}

// PackageLoader facade that delegates to the correct loader based on the pattern.
type PackageLoader struct {
	registrations []loaderRegistration
	logger        *console.Logger
}

type loaderRegistration struct {
	format   packageFileFormat
	loader   Loader
	patterns []string
}

type packageFileFormat string

const (
	jsonPackageFile     packageFileFormat = "json"
	yamlPackageFile     packageFileFormat = "yaml"
	makePackageFile     packageFileFormat = "makefile"
	pklPackageFile      packageFileFormat = "pkl"
	starlarkPackageFile packageFileFormat = "starlark"
	scriptPackageFile   packageFileFormat = "script"
)

func loaderRegistrations() []loaderRegistration {
	return []loaderRegistration{
		{format: jsonPackageFile, loader: JsonLoader{}, patterns: []string{"BUILD.json"}},
		{format: yamlPackageFile, loader: YamlLoader{}, patterns: []string{"BUILD.yaml", "BUILD.yml"}},
		{format: makePackageFile, loader: MakefileLoader{}, patterns: []string{"Makefile"}},
		{format: pklPackageFile, loader: &PklLoader{}, patterns: []string{"BUILD.pkl"}},
		{format: starlarkPackageFile, loader: StarlarkLoader{}, patterns: []string{"BUILD.star", "BUILD.bzl"}},
		{format: scriptPackageFile, loader: ScriptLoader{}, patterns: []string{"*.grog.sh", "*.grog.py"}},
	}
}

func (registration loaderRegistration) matches(fileName string) bool {
	for _, pattern := range registration.patterns {
		if matched, _ := filepath.Match(pattern, fileName); matched {
			return true
		}
	}
	return false
}

func NewPackageLoader(logger *console.Logger) *PackageLoader {
	return &PackageLoader{
		logger:        logger,
		registrations: loaderRegistrations(),
	}
}

// PackageFilePatterns returns the file patterns handled by production loaders.
func PackageFilePatterns() []string {
	patterns := []string{}
	for _, registration := range loaderRegistrations() {
		patterns = append(patterns, registration.patterns...)
	}
	return patterns
}

// IsPackageFile reports whether a production loader accepts a file name.
func IsPackageFile(fileName string) bool {
	for _, registration := range loaderRegistrations() {
		if registration.matches(fileName) {
			return true
		}
	}
	return false
}

func isPackageFileFormat(fileName string, format packageFileFormat) bool {
	for _, registration := range loaderRegistrations() {
		if registration.format == format && registration.matches(fileName) {
			return true
		}
	}
	return false
}

// IsYAMLPackageFile reports whether the YAML loader accepts a file name.
func IsYAMLPackageFile(fileName string) bool {
	return isPackageFileFormat(fileName, yamlPackageFile)
}

// IsStarlarkPackageFile reports whether the Starlark loader accepts a file name.
func IsStarlarkPackageFile(fileName string) bool {
	return isPackageFileFormat(fileName, starlarkPackageFile)
}

// LoadPackageFile loads a file through the production loader registry.
func LoadPackageFile(loadContext context.Context, filePath string) (PackageDTO, bool, error) {
	fileName := filepath.Base(filePath)
	for _, registration := range loaderRegistrations() {
		if registration.matches(fileName) {
			packageDTO, matched, operationError := registration.loader.Load(loadContext, filePath)
			packageDTO.SourceFilePath = filePath
			return packageDTO, matched, operationError
		}
	}
	return PackageDTO{}, false, nil
}

// LoadIfMatched loads the package from the specified file name if it matches any of the supported file names.
func (p *PackageLoader) LoadIfMatched(ctx context.Context, filePath string, fileName string) (PackageDTO, bool, error) {
	for _, registration := range p.registrations {
		if registration.matches(fileName) {
			p.logger.Debugf("Loading package from %s using loader %s", filePath, registration.loader)
			packageDTO, matched, err := registration.loader.Load(ctx, filePath)
			packageDTO.SourceFilePath = filePath
			return packageDTO, matched, err
		}
	}

	return PackageDTO{}, false, nil
}
