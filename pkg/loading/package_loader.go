package loading

import (
	"grog/pkg/model"
	"slices"
)

// Loader Implement this to provide a loader for a user provided BUILD file format
type Loader interface {
	// FileNames returns the supported file names for this loader
	FileNames() []string
	// Load reads the file at the specified filePath and unmarshals its content into a model.Package
	Load(filePath string) (model.Package, error)
}

// PackageLoader facade that delegates to the correct loader based on the pattern
type PackageLoader struct {
	loaders   []Loader
	fileNames []string
}

func NewPackageLoader() *PackageLoader {
	return &PackageLoader{
		loaders: []Loader{
			JSONLoader{},
		},
	}
}

// LoadIfMatched loads the package from the specified file name if it matches any of the supported file names.
func (p *PackageLoader) LoadIfMatched(filePath string, fileName string) (model.Package, bool, error) {
	for _, loader := range p.loaders {
		if slices.Contains(loader.FileNames(), fileName) {
			pkg, err := loader.Load(filePath)
			return pkg, true, err
		}
	}

	return model.Package{}, false, nil
}
