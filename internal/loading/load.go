package loading

import (
	"fmt"
	"github.com/charlievieth/fastwalk"
	"github.com/spf13/viper"
	"go.uber.org/zap"
	"grog/internal/config"
	"grog/internal/label"
	"grog/internal/model"
	"io/fs"
)

func LoadPackages(logger *zap.SugaredLogger) ([]*model.Package, error) {
	workspaceRoot := viper.Get("workspace_root").(string)

	var packages []*model.Package

	conf := fastwalk.Config{
		// Don't follow symlinks
		Follow: false,
	}

	packageLoader := NewPackageLoader(logger)

	walkFn := func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			// TODO do we want to collect all loading errors first? Seems like a better dev-ex
			// Idea: collect errors like so: https://github.com/hashicorp/go-multierror
			return err // returning the error stops iteration
		}

		// TODO Apparently, d can be nil for some reason
		if d == nil {
			return fmt.Errorf(
				"d is nil for path %s",
				path)
		}

		pkgDto, matched, err := packageLoader.LoadIfMatched(path, d.Name())
		if err != nil {
			return err
		}

		if matched {
			packagePath, err := config.GetPackagePath(path)
			if err != nil {
				return err
			}

			pkg, err := getEnrichedPackage(packagePath, pkgDto)
			if err != nil {
				return err
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

// getEnrichedPackage adds the package path to the target labels and returns the enriched package
func getEnrichedPackage(packagePath string, pkg PackageDTO) (*model.Package, error) {
	targets := make(map[label.TargetLabel]*model.Target)
	for targetName, target := range pkg.Targets {
		var deps []label.TargetLabel
		// parse labels
		for _, dep := range target.Deps {
			depLabel, err := label.ParseTargetLabel(packagePath, dep)
			if err != nil {
				return nil, err
			}
			deps = append(deps, depLabel)
		}

		// root package should be understood as ""
		if packagePath == "." {
			packagePath = ""
		}
		targetLabel := label.TargetLabel{Package: packagePath, Name: targetName}
		if _, ok := targets[targetLabel]; ok {
			return nil, fmt.Errorf("duplicate target label: %s (package file %s)", targetName, pkg.SourceFilePath)
		}

		targets[targetLabel] = &model.Target{
			Label:   targetLabel,
			Command: target.Command,
			Deps:    deps,
			Inputs:  target.Inputs,
			Outputs: target.Outputs,
		}
	}

	return &model.Package{
		SourceFilePath: pkg.SourceFilePath,
		Targets:        targets,
	}, nil
}
