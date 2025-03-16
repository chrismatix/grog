package loading

import (
	"fmt"
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
			logger.Warn("%s: %v\n", path, err)
			return nil // returning the error stops iteration
		}

		pkg, matched, err := packageLoader.LoadIfMatched(path)
		if matched && err == nil {
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

		return err
	}

	if err := fastwalk.Walk(&conf, workspaceRoot, walkFn); err != nil {
		return nil, fmt.Errorf("%s: %v\n", workspaceRoot, err)
	}

	return packages, nil
}
