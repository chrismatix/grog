package loading

import (
	"context"

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

// PackageLoader facade that delegates to the correct loader based on the pattern
type PackageLoader struct {
	loaders   []Loader
	fileNames []string
	logger    *console.Logger
}

func NewPackageLoader(logger *console.Logger) *PackageLoader {
	return &PackageLoader{
		logger: logger,
		// register loaders here
		loaders: []Loader{
			JsonLoader{},
			YamlLoader{},
			MakefileLoader{},
			&PklLoader{},
			StarlarkLoader{},
			ScriptLoader{},
		},
	}
}

// LoadIfMatched loads the package from the specified file name if it matches any of the supported file names.
func (p *PackageLoader) LoadIfMatched(ctx context.Context, filePath string, fileName string) (PackageDTO, bool, error) {
	for _, loader := range p.loaders {
		if loader.Matches(fileName) {
			p.logger.Debugf("Loading package from %s using loader %s", filePath, loader)
			packageDTO, matched, err := loader.Load(ctx, filePath)
			packageDTO.SourceFilePath = filePath
			return packageDTO, matched, err
		}
	}

	return PackageDTO{}, false, nil
}
