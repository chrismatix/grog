package loading

import (
	"context"
	"fmt"
	"github.com/boyter/gocodewalker"
	"grog/internal/config"
	"grog/internal/console"
	"grog/internal/model"
	"slices"
	"sync"
)

func LoadPackages(ctx context.Context, dir string) ([]*model.Package, error) {
	workspaceRoot := config.Global.WorkspaceRoot
	logger := console.GetLogger(ctx)

	startDir := workspaceRoot
	if dir != "" {
		startDir = config.GetPathAbsoluteToWorkspaceRoot(dir)
	}

	fileListQueue := make(chan *gocodewalker.File, 100)

	fileWalker := gocodewalker.NewParallelFileWalker([]string{startDir}, fileListQueue)
	go fileWalker.Start()

	packageLoader := NewPackageLoader(logger)

	// Keep track of loaded package paths to error out when there is a collision
	// e.g. when a user defines both BUILD.json and BUILD.py in the same directory
	// packagePath -> sourceFilePath
	loadedPackagePaths := make(map[string]string)
	loadedMu := &sync.Mutex{}

	var packages []*model.Package

	for f := range fileListQueue {
		// TODO this should be processed in a worker as well

		pkgDto, matched, err := packageLoader.LoadIfMatched(ctx, f.Location, f.Filename)
		if err != nil {
			return nil, err
		}

		if matched {
			packagePath, err := config.GetPackagePath(f.Location)
			if err != nil {
				return nil, err
			}

			pkg, err := getEnrichedPackage(logger, packagePath, pkgDto)
			if err != nil {
				fmt.Println(err)
				return nil, err
			}

			// Check for duplicate package definitions
			loadedMu.Lock()
			if loadedPackageFile, ok := loadedPackagePaths[packagePath]; ok {
				loadedMu.Unlock()

				// Sort the paths to make the error message deterministic and testable via integration test
				paths := []string{
					config.MustGetPathRelativeToWorkspaceRoot(loadedPackageFile),
					config.MustGetPathRelativeToWorkspaceRoot(pkg.SourceFilePath),
				}
				slices.Sort(paths)
				return nil, fmt.Errorf("found conflicting package definitions at package path: %s\n- %s\n- %s",
					packagePath,
					paths[0],
					paths[1],
				)
			}
			loadedPackagePaths[packagePath] = pkgDto.SourceFilePath
			loadedMu.Unlock()

			packages = append(packages, pkg)
		}
	}

	return packages, nil
}
