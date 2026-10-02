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

func LoadAllPackages(ctx context.Context, inferrer *DependencyInferrer) ([]*model.Package, error) {
	packages, loadError := LoadPackages(ctx, config.Global.WorkspaceRoot)
	if loadError != nil {
		return nil, loadError
	}
	return inferrer.inferDependencies(ctx, packages)
}

// LoadPackages loads all packages in the given directory and its subdirectories.
func LoadPackages(ctx context.Context, startDir string) ([]*model.Package, error) {
	logger := console.GetLogger(ctx)

	fileListQueue := make(chan *gocodewalker.File, 100)

	fileWalker := gocodewalker.NewParallelFileWalker([]string{startDir}, fileListQueue)
	fileWalker.IncludeHidden = config.Global.IncludeHidden
	go func() { _ = fileWalker.Start() }()

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

	if into.DependencyResolvers == nil {
		into.DependencyResolvers = make(map[label.TargetLabel]*model.DependencyResolver)
	}
	for resolverLabel, resolver := range from.DependencyResolvers {
		if kind, sourceFilePath, exists := existingDefinition(into, resolverLabel); exists {
			return fmt.Errorf("duplicate dependency resolver label: %s (defined in %s and as %s in %s)", resolverLabel, resolver.SourceFilePath, kind, sourceFilePath)
		}
		into.DependencyResolvers[resolverLabel] = resolver
	}

	for targetLabel, target := range from.Targets {
		if kind, sourceFilePath, exists := existingDefinition(into, targetLabel); exists {
			return fmt.Errorf("duplicate target label: %s (defined in %s and as %s in %s)", targetLabel, target.SourceFilePath, kind, sourceFilePath)
		}
		into.Targets[targetLabel] = target
	}

	for aliasLabel, alias := range from.Aliases {
		if kind, sourceFilePath, exists := existingDefinition(into, aliasLabel); exists {
			return fmt.Errorf("duplicate alias label: %s (defined in %s and as %s in %s)", aliasLabel, alias.SourceFilePath, kind, sourceFilePath)
		}
		into.Aliases[aliasLabel] = alias
	}

	for resourceLabel, resource := range from.Resources {
		if kind, sourceFilePath, exists := existingDefinition(into, resourceLabel); exists {
			return fmt.Errorf("duplicate resource label: %s (defined in %s and as %s in %s)", resourceLabel, resource.SourceFilePath, kind, sourceFilePath)
		}
		into.Resources[resourceLabel] = resource
	}

	return nil
}

// existingDefinition reports which kind of definition in pkg already uses the label and where it is defined.
func existingDefinition(pkg *model.Package, targetLabel label.TargetLabel) (kind string, sourceFilePath string, exists bool) {
	if target, exists := pkg.Targets[targetLabel]; exists {
		return "target", target.SourceFilePath, true
	}
	if alias, exists := pkg.Aliases[targetLabel]; exists {
		return "alias", alias.SourceFilePath, true
	}
	if resource, exists := pkg.Resources[targetLabel]; exists {
		return "resource", resource.SourceFilePath, true
	}
	if resolver, exists := pkg.DependencyResolvers[targetLabel]; exists {
		return "dependency resolver", resolver.SourceFilePath, true
	}
	return "", "", false
}
