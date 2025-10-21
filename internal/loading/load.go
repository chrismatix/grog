package loading

import (
	"context"
	"fmt"
	"grog/internal/config"
	"grog/internal/console"
	"grog/internal/label"
	"grog/internal/model"
	"sync"

	"github.com/boyter/gocodewalker"
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
	loadedPackages := make(map[string]*model.Package)
	loadedMu := &sync.Mutex{}

	for f := range fileListQueue {
		// TODO this should be processed in a worker as well

		pkgDto, matched, err := packageLoader.LoadIfMatched(ctx, f.Location, f.Filename)
		if err != nil {
			return nil, err
		}

		if !matched {
			continue
		}

		packagePath, err := config.GetPackagePath(f.Location)
		if err != nil {
			return nil, err
		}

		pkg, err := getEnrichedPackage(logger, packagePath, pkgDto)
		if err != nil {
			fmt.Println(err)
			return nil, err
		}

		// Merge into existing package if it exists or set
		loadedMu.Lock()
		if existingPackage, ok := loadedPackages[packagePath]; ok {
			// This mutates the existingPackage
			mergingErr := mergePackages(pkg, existingPackage)
			if mergingErr != nil {
				return nil, mergingErr
			}
		} else {
			loadedPackages[packagePath] = pkg
		}
		loadedMu.Unlock()
	}

	packages := make([]*model.Package, 0, len(loadedPackages))
	for _, pkg := range loadedPackages {
		packages = append(packages, pkg)
	}

	return packages, nil
}

func mergePackages(from *model.Package, into *model.Package) error {
	if into.Targets == nil {
		into.Targets = make(map[label.TargetLabel]*model.Target)
	}
	if into.Aliases == nil {
		into.Aliases = make(map[label.TargetLabel]*model.Alias)
	}

	for fromTargetLabel, fromTarget := range from.Targets {
		if intoTarget, exists := into.Targets[fromTargetLabel]; exists {
			return fmt.Errorf("duplicate target label: %s (defined in %s and %s)", fromTargetLabel, intoTarget.SourceFilePath, fromTarget.SourceFilePath)
		}
		into.Targets[fromTargetLabel] = fromTarget
	}

	for fromAliasLabel, fromAlias := range from.Aliases {
		if intoAlias, exists := into.Aliases[fromAliasLabel]; exists {
			return fmt.Errorf("duplicate target label: %s (defined in %s and %s)", fromAliasLabel, intoAlias.SourceFilePath, fromAlias.SourceFilePath)
		}
		if intoTarget, exists := into.Targets[fromAliasLabel]; exists {
			return fmt.Errorf("duplicate alias label: %s (defined in %s and as target in %s)", fromAliasLabel, fromAlias.SourceFilePath, intoTarget.SourceFilePath)
		}
		into.Aliases[fromAliasLabel] = fromAlias
	}

	return nil
}
