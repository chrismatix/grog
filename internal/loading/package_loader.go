package loading

import (
	"go.uber.org/zap"
	"slices"
)

// Loader Implement this to provide a loader for a user provided BUILD file format
type Loader interface {
	// FileNames returns the supported file names for this loader
	FileNames() []string
	// Load reads the file at the specified filePath and unmarshals its content into a model.Package
	// Returns true if the file contains a valid package definition (needed for Makefiles)
	Load(filePath string) (PackageDTO, bool, error)
}

// PackageLoader facade that delegates to the correct loader based on the pattern
type PackageLoader struct {
	loaders   []Loader
	fileNames []string
	logger    *zap.SugaredLogger
}

func NewPackageLoader(logger *zap.SugaredLogger) *PackageLoader {
	return &PackageLoader{
		logger: logger,
		loaders: []Loader{
			JsonLoader{},
			YamlLoader{},
			MakefileLoader{},
		},
	}
}

// LoadIfMatched loads the package from the specified file name if it matches any of the supported file names.
func (p *PackageLoader) LoadIfMatched(filePath string, fileName string) (PackageDTO, bool, error) {
	for _, loader := range p.loaders {
		if slices.Contains(loader.FileNames(), fileName) {
			p.logger.Debugf("Loading package from %s using loader %s", filePath, loader)
			pkgDto, matched, err := loader.Load(filePath)
			pkgDto.SourceFilePath = filePath
			return pkgDto, matched, err
		}
	}

	return PackageDTO{}, false, nil
}
