package loading

import (
	"context"
	"fmt"
	"grog/internal/config"
	"grog/internal/console"
	"grog/internal/label"
	"grog/internal/model"
	"runtime"
	"sync"

	"github.com/boyter/gocodewalker"
)

func LoadAllPackages(ctx context.Context) ([]*model.Package, error) {
	packages, err := LoadPackages(ctx, config.Global.WorkspaceRoot)
	if err != nil {
		return nil, err
	}
	if err := inferDependencies(ctx, packages); err != nil {
		return nil, err
	}
	return packages, nil
}

// LoadPackages loads all packages in the given directory and its subdirectories.
func LoadPackages(ctx context.Context, startDir string) ([]*model.Package, error) {
	logger := console.GetLogger(ctx)

	fileListQueue := make(chan *gocodewalker.File, 100)

	fileWalker := gocodewalker.NewParallelFileWalker([]string{startDir}, fileListQueue)
	fileWalker.IncludeHidden = config.Global.IncludeHidden
	go fileWalker.Start()

	packageLoader := NewPackageLoader(logger)

	// Keep track of loaded package paths to error out when there is a collision
	// e.g. when a user defines both BUILD.json and BUILD.py in the same directory
	// packagePath -> sourceFilePath
	loadedPackages := make(map[string]*model.Package)
	var loadedMutex sync.Mutex

	loadContext, cancel := context.WithCancel(ctx)
	defer cancel()

	workerCount := config.Global.NumWorkers
	if workerCount < 1 {
		workerCount = runtime.NumCPU()
	}

	var errorOnce sync.Once
	var loadError error
	setError := func(err error) {
		if err == nil {
			return
		}
		errorOnce.Do(func() {
			loadError = err
			fmt.Println(err)
			cancel()
		})
	}

	var waitGroup sync.WaitGroup
	waitGroup.Add(workerCount)

	for workerIndex := 0; workerIndex < workerCount; workerIndex++ {
		go func() {
			defer waitGroup.Done()
			for fileEntry := range fileListQueue {
				if loadContext.Err() != nil {
					continue
				}

				packageDTO, matched, err := packageLoader.LoadIfMatched(loadContext, fileEntry.Location, fileEntry.Filename)
				if err != nil {
					setError(err)
					continue
				}

				if !matched {
					continue
				}

				packagePath, err := config.GetPackagePath(fileEntry.Location)
				if err != nil {
					setError(err)
					continue
				}

				packageModel, err := getEnrichedPackage(logger, packagePath, packageDTO)
				if err != nil {
					setError(err)
					continue
				}

				// Merge into existing package if it exists or set
				loadedMutex.Lock()
				existingPackage, ok := loadedPackages[packagePath]
				if ok {
					// This mutates the existingPackage
					mergeError := mergePackages(packageModel, existingPackage)
					loadedMutex.Unlock()
					if mergeError != nil {
						setError(mergeError)
					}
					continue
				}

				loadedPackages[packagePath] = packageModel
				loadedMutex.Unlock()
			}
		}()
	}

	waitGroup.Wait()
	if loadError != nil {
		return nil, loadError
	}
	if contextErr := loadContext.Err(); contextErr != nil {
		return nil, contextErr
	}

	packages := make([]*model.Package, 0, len(loadedPackages))
	for _, loadedPackage := range loadedPackages {
		packages = append(packages, loadedPackage)
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
	if into.Resources == nil {
		into.Resources = make(map[label.TargetLabel]*model.Resource)
	}

	if into.DependencyProviders == nil {
		into.DependencyProviders = make(map[label.TargetLabel]*model.DependencyProvider)
	}
	for providerLabel, provider := range from.DependencyProviders {
		if existing := into.DependencyProviders[providerLabel]; existing != nil {
			return fmt.Errorf("duplicate dependency provider label: %s (defined in %s and %s)", providerLabel, existing.SourceFilePath, provider.SourceFilePath)
		}
		if existing := into.Targets[providerLabel]; existing != nil {
			return fmt.Errorf("duplicate dependency provider label: %s (defined in %s and as target in %s)", providerLabel, provider.SourceFilePath, existing.SourceFilePath)
		}
		if existing := into.Aliases[providerLabel]; existing != nil {
			return fmt.Errorf("duplicate dependency provider label: %s (defined in %s and as alias in %s)", providerLabel, provider.SourceFilePath, existing.SourceFilePath)
		}
		if existing := into.Resources[providerLabel]; existing != nil {
			return fmt.Errorf("duplicate dependency provider label: %s (defined in %s and as resource in %s)", providerLabel, provider.SourceFilePath, existing.SourceFilePath)
		}
		into.DependencyProviders[providerLabel] = provider
	}

	for fromTargetLabel, fromTarget := range from.Targets {
		if existing := into.DependencyProviders[fromTargetLabel]; existing != nil {
			return fmt.Errorf("duplicate target label: %s (defined in %s and as dependency provider in %s)", fromTargetLabel, fromTarget.SourceFilePath, existing.SourceFilePath)
		}
		if intoTarget, exists := into.Targets[fromTargetLabel]; exists {
			return fmt.Errorf("duplicate target label: %s (defined in %s and %s)", fromTargetLabel, intoTarget.SourceFilePath, fromTarget.SourceFilePath)
		}
		if intoAlias, exists := into.Aliases[fromTargetLabel]; exists {
			return fmt.Errorf("duplicate target label: %s (defined in %s and as alias in %s)", fromTargetLabel, fromTarget.SourceFilePath, intoAlias.SourceFilePath)
		}
		if intoResource, exists := into.Resources[fromTargetLabel]; exists {
			return fmt.Errorf("duplicate target label: %s (defined in %s and as resource in %s)", fromTargetLabel, fromTarget.SourceFilePath, intoResource.SourceFilePath)
		}
		into.Targets[fromTargetLabel] = fromTarget
	}

	for fromAliasLabel, fromAlias := range from.Aliases {
		if existing := into.DependencyProviders[fromAliasLabel]; existing != nil {
			return fmt.Errorf("duplicate alias label: %s (defined in %s and as dependency provider in %s)", fromAliasLabel, fromAlias.SourceFilePath, existing.SourceFilePath)
		}
		if intoAlias, exists := into.Aliases[fromAliasLabel]; exists {
			return fmt.Errorf("duplicate target label: %s (defined in %s and %s)", fromAliasLabel, intoAlias.SourceFilePath, fromAlias.SourceFilePath)
		}
		if intoTarget, exists := into.Targets[fromAliasLabel]; exists {
			return fmt.Errorf("duplicate alias label: %s (defined in %s and as target in %s)", fromAliasLabel, fromAlias.SourceFilePath, intoTarget.SourceFilePath)
		}
		if intoResource, exists := into.Resources[fromAliasLabel]; exists {
			return fmt.Errorf("duplicate alias label: %s (defined in %s and as resource in %s)", fromAliasLabel, fromAlias.SourceFilePath, intoResource.SourceFilePath)
		}
		into.Aliases[fromAliasLabel] = fromAlias
	}

	for fromResourceLabel, fromResource := range from.Resources {
		if existing := into.DependencyProviders[fromResourceLabel]; existing != nil {
			return fmt.Errorf("duplicate resource label: %s (defined in %s and as dependency provider in %s)", fromResourceLabel, fromResource.SourceFilePath, existing.SourceFilePath)
		}
		if intoResource, exists := into.Resources[fromResourceLabel]; exists {
			return fmt.Errorf("duplicate resource label: %s (defined in %s and %s)", fromResourceLabel, intoResource.SourceFilePath, fromResource.SourceFilePath)
		}
		if intoTarget, exists := into.Targets[fromResourceLabel]; exists {
			return fmt.Errorf("duplicate resource label: %s (defined in %s and as target in %s)", fromResourceLabel, fromResource.SourceFilePath, intoTarget.SourceFilePath)
		}
		if intoAlias, exists := into.Aliases[fromResourceLabel]; exists {
			return fmt.Errorf("duplicate resource label: %s (defined in %s and as alias in %s)", fromResourceLabel, fromResource.SourceFilePath, intoAlias.SourceFilePath)
		}
		into.Resources[fromResourceLabel] = fromResource
	}

	return nil
}
