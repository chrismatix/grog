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

	if err := validateScheduling(packages); err != nil {
		return nil, err
	}

	return packages, nil
}

// validateScheduling verifies that each target's weight fits within the global
// worker pool and, when the target participates in a concurrency group, within
// that group's capacity. These are misconfigurations that would otherwise
// deadlock the scheduler at runtime, so fail loading instead.
func validateScheduling(packages []*model.Package) error {
	numWorkers := config.Global.NumWorkers
	if numWorkers < 1 {
		numWorkers = runtime.NumCPU()
	}

	for _, pkg := range packages {
		for _, target := range pkg.Targets {
			if target.Weight > numWorkers {
				return fmt.Errorf(
					"target %s declares weight=%d but num_workers=%d",
					target.Label, target.Weight, numWorkers,
				)
			}
			if target.ConcurrencyGroup == "" {
				continue
			}
			capacity := GroupCapacity(target.ConcurrencyGroup)
			if target.Weight > capacity {
				return fmt.Errorf(
					"target %s declares weight=%d in concurrency_group %q but group capacity=%d",
					target.Label, target.Weight, target.ConcurrencyGroup, capacity,
				)
			}
		}
	}
	return nil
}

// GroupCapacity returns the configured capacity for a concurrency group,
// defaulting to 1 when the group is absent from config or misconfigured.
func GroupCapacity(name string) int {
	if capacity, ok := config.Global.ConcurrencyGroups[name]; ok && capacity > 0 {
		return capacity
	}
	return 1
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
