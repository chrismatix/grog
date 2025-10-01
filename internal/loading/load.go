package loading

import (
	"context"
	"fmt"
	"github.com/boyter/gocodewalker"
	"grog/internal/config"
	"grog/internal/console"
	"grog/internal/model"
	"maps"
	"slices"
	"sync"
)

func LoadAllPackages(ctx context.Context) ([]*model.Package, error) {
	return LoadPackages(ctx, config.Global.WorkspaceRoot)
}

// LoadPackages loads all packages in the given directory and its subdirectories.
func LoadPackages(ctx context.Context, startDir string) ([]*model.Package, error) {
	logger := console.GetLogger(ctx)

	fileListQueue := make(chan *gocodewalker.File, 100)

	fileWalker := gocodewalker.NewParallelFileWalker([]string{startDir}, fileListQueue)
	go fileWalker.Start()

	packageLoader := NewPackageLoader(logger)

	// Keep track of loaded package paths to error out when there is a collision
	// e.g. when a user defines both BUILD.json and BUILD.py in the same directory
	// packagePath -> sourceFilePath
	loadedPackagePaths := make(map[string]string)
	loadedMu := &sync.Mutex{}

	packagesByPath := make(map[string]*model.Package)
	scriptLoader := ScriptLoader{}

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

			loadedMu.Lock()
			if existing, ok := packagesByPath[packagePath]; ok {
				if source, exists := loadedPackagePaths[packagePath]; exists {
					loadedMu.Unlock()

					paths := []string{
						config.MustGetPathRelativeToWorkspaceRoot(source),
						config.MustGetPathRelativeToWorkspaceRoot(pkg.SourceFilePath),
					}
					slices.Sort(paths)
					return nil, fmt.Errorf("found conflicting package definitions at package path: %s\n- %s\n- %s",
						packagePath,
						paths[0],
						paths[1],
					)
				}

				if err := mergePackages(pkg, existing); err != nil {
					loadedMu.Unlock()
					return nil, err
				}
			}
			packagesByPath[packagePath] = pkg
			loadedPackagePaths[packagePath] = pkgDto.SourceFilePath
			loadedMu.Unlock()
			continue
		}

		if !scriptLoader.Matches(f.Filename) {
			continue
		}

		packagePath, err := config.GetPackagePath(f.Location)
		if err != nil {
			return nil, err
		}

		scriptPkgDto, matched, err := scriptLoader.Load(ctx, f.Location)
		if err != nil {
			return nil, err
		}
		if !matched {
			relativePath := config.MustGetPathRelativeToWorkspaceRoot(f.Location)
			return nil, fmt.Errorf("%s does not contain a # @grog annotation", relativePath)
		}

		scriptPkg, err := getEnrichedPackage(logger, packagePath, scriptPkgDto)
		if err != nil {
			return nil, err
		}

		loadedMu.Lock()
		if existing, ok := packagesByPath[packagePath]; ok {
			if err := mergePackages(existing, scriptPkg); err != nil {
				loadedMu.Unlock()
				return nil, err
			}
		} else {
			packagesByPath[packagePath] = scriptPkg
		}
		loadedMu.Unlock()
	}

	packages := slices.Collect(maps.Values(packagesByPath))

	return packages, nil
}
