package loading

import (
	"fmt"
	"github.com/charlievieth/fastwalk"
	"github.com/spf13/viper"
	"grog/pkg/model"
	"io/fs"
	"os"
)

func LoadPackages() ([]model.Package, error) {
	workspaceRoot := viper.Get("workspace_root").(string)

	var packages []model.Package

	// Follow links if the "-L" flag is provided
	conf := fastwalk.Config{
		Follow: false,
	}

	packageLoader := NewPackageLoader()

	walkFn := func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			fmt.Fprintf(os.Stderr, "%s: %v\n", path, err)
			return nil // returning the error stops iteration
		}

		pkg, matched, err := packageLoader.LoadIfMatched(path)
		if matched && err == nil {
			packages = append(packages, pkg)
		}

		return err
	}

	if err := fastwalk.Walk(&conf, workspaceRoot, walkFn); err != nil {
		return nil, fmt.Errorf("%s: %v\n", workspaceRoot, err)
	}

	return packages, nil
}
