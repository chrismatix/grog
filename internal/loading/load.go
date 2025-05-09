package loading

import (
	"context"
	"fmt"
	"github.com/bmatcuk/doublestar/v4"
	"github.com/boyter/gocodewalker"
	"go.uber.org/zap"
	"grog/internal/config"
	"grog/internal/console"
	"grog/internal/label"
	"grog/internal/model"
	"grog/internal/output"
	"os"
	"slices"
	"strings"
	"sync"
)

func LoadPackages(ctx context.Context) ([]*model.Package, error) {
	workspaceRoot := config.Global.WorkspaceRoot
	logger := console.GetLogger(ctx)

	fileListQueue := make(chan *gocodewalker.File, 100)

	fileWalker := gocodewalker.NewParallelFileWalker([]string{workspaceRoot}, fileListQueue)
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

// getEnrichedPackage enriches the parsing dto with the following information
// - adds the package path to the target labels
// - resolves the globs in the inputs TODO
// - parses the deps into target labels
func getEnrichedPackage(logger *zap.SugaredLogger, packagePath string, pkg PackageDTO) (*model.Package, error) {
	targets := make(map[label.TargetLabel]*model.Target)
	absolutePackagePath := config.GetPathAbsoluteToWorkspaceRoot(packagePath)

	for _, target := range pkg.Targets {
		var deps []label.TargetLabel
		// parse labels
		for _, dep := range target.Dependencies {
			depLabel, err := label.ParseTargetLabel(packagePath, dep)
			if err != nil {
				return nil, err
			}
			deps = append(deps, depLabel)
		}

		// root package is always encoded as ""
		if packagePath == "." {
			packagePath = ""
		}
		targetLabel := label.TargetLabel{Package: packagePath, Name: target.Name}
		if _, ok := targets[targetLabel]; ok {
			return nil, fmt.Errorf("duplicate target label: %s (package file %s)", target.Name, pkg.SourceFilePath)
		}

		resolvedInputs, err := resolveInputs(logger, absolutePackagePath, target.Inputs)
		if err != nil {
			return nil, fmt.Errorf("failed to resolve inputs for target %s: %w", targetLabel, err)
		}

		parsedOutputs, err := output.ParseOutputs(target.Outputs)
		if err != nil {
			return nil, fmt.Errorf("failed to parse outputs for target %s: %w", targetLabel, err)
		}

		parsedBinOutput := model.Output{}
		if target.BinOutput != "" {
			parsedBinOutput, err = output.ParseOutput(target.BinOutput)
			if err != nil {
				return nil, fmt.Errorf("failed to parse bin output for target %s: %w", targetLabel, err)
			}
			if parsedBinOutput.IsFile() == false {
				return nil, fmt.Errorf("bin output %s for target %s must be of type file",
					target.BinOutput, targetLabel)
			}
		}

		if _, ok := targets[targetLabel]; ok {
			return nil, fmt.Errorf("duplicate target label: %s (package file %s)", target.Name, pkg.SourceFilePath)
		}

		targets[targetLabel] = &model.Target{
			Label:        targetLabel,
			Command:      target.Command,
			Dependencies: deps,
			Inputs:       resolvedInputs,
			Outputs:      parsedOutputs,
			BinOutput:    parsedBinOutput,
			Platform:     target.Platform,
			Tags:         target.Tags,
		}
	}

	return &model.Package{
		SourceFilePath: pkg.SourceFilePath,
		Targets:        targets,
	}, nil
}

func resolveInputs(
	logger *zap.SugaredLogger,
	absolutePackagePath string,
	inputs []string,
) ([]string, error) {
	var resolvedInputs []string
	fsys := os.DirFS(absolutePackagePath)

	for _, input := range inputs {
		if !strings.Contains(input, "*") {
			// Nothing to resolve
			resolvedInputs = append(resolvedInputs, input)
			continue
		}

		// Match files using doublestar
		matches, err := doublestar.Glob(fsys, input)
		if err != nil {
			return nil, fmt.Errorf("failed to resolve glob pattern %s: %w", input, err)
		}
		logger.Debugf("Resolved glob pattern %s in %s to %v", input, absolutePackagePath, matches)

		// Append matched files to the resolvedInputs
		resolvedInputs = append(resolvedInputs, matches...)
	}

	return resolvedInputs, nil
}
