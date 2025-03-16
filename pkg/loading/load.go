package loading

import (
	"github.com/charlievieth/fastwalk"
	"github.com/spf13/viper"
	"go.uber.org/zap"
	"grog/pkg/config"
	"grog/pkg/label"
	"grog/pkg/model"
	"io/fs"
)

func LoadPackages(logger *zap.SugaredLogger) ([]model.Package, error) {
	workspaceRoot := viper.Get("workspace_root").(string)

	var packages []model.Package

	// Follow links if the "-L" flag is provided
	conf := fastwalk.Config{
		Follow: false,
	}

	packageLoader := NewPackageLoader()

	walkFn := func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			// TODO do we want to collect all loading errors first? Seems like a better dev-ex
			return err // returning the error stops iteration
		}

		pkg, matched, err := packageLoader.LoadIfMatched(path, d.Name())
		if err != nil {
			return err
		}

		if matched {
			packagePath, err := config.GetPathRelativeToWorkspaceRoot(path)
			if err != nil {
				return err
			}

			// attach the TargetLabel to each target in the package
			for _, t := range pkg.Targets {
				t.Label = label.TargetLabel{Package: packagePath, Name: t.Name}
			}

			packages = append(packages, pkg)
		}

		return nil
	}

	if err := fastwalk.Walk(&conf, workspaceRoot, walkFn); err != nil {
		return nil, err
	}

	return packages, nil
}
