package loading

import (
	"fmt"
	"github.com/bmatcuk/doublestar/v4"
	"go.uber.org/zap"
	"grog/internal/config"
	"grog/internal/label"
	"grog/internal/model"
	"grog/internal/output"
	"os"
	"strings"
	"time"
)

// getEnrichedPackage enriches the parsing dto with the following information
// - adds the package path to the target labels
// - resolves the globs in the inputs
// - applies any defaults
// - parses the deps into target labels
func getEnrichedPackage(logger *zap.SugaredLogger, packagePath string, pkg PackageDTO) (*model.Package, error) {
	targets := make(map[label.TargetLabel]*model.Target)
	aliases := make(map[label.TargetLabel]*model.Alias)
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

		resolvedInputs, err := resolveInputs(logger, absolutePackagePath, target.Inputs, target.ExcludeInputs)
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

		var timeout time.Duration
		if target.Timeout != "" {
			timeout, err = time.ParseDuration(target.Timeout)
			if err != nil {
				return nil, fmt.Errorf("failed to parse timeout for target %s: %w", targetLabel, err)
			}
		}

		// Determine the platform to use
		// If target has its own platform, use that
		// Otherwise, use the package default platform if available
		targetPlatform := target.Platform
		if targetPlatform == nil && pkg.DefaultPlatform != nil {
			targetPlatform = pkg.DefaultPlatform
		}

		targets[targetLabel] = &model.Target{
			Label:                targetLabel,
			Command:              target.Command,
			Dependencies:         deps,
			Inputs:               resolvedInputs,
			UnresolvedInputs:     target.Inputs,
			ExcludeInputs:        target.ExcludeInputs,
			Outputs:              parsedOutputs,
			BinOutput:            parsedBinOutput,
			Platform:             targetPlatform,
			OutputChecks:         target.OutputChecks,
			Tags:                 target.Tags,
			EnvironmentVariables: target.EnvironmentVariables,
			Timeout:              timeout,
		}
	}

	for _, alias := range pkg.Aliases {
		actualLabel, err := label.ParseTargetLabel(packagePath, alias.Actual)
		if err != nil {
			return nil, err
		}

		if packagePath == "." {
			packagePath = ""
		}
		aliasLabel := label.TargetLabel{Package: packagePath, Name: alias.Name}
		if _, ok := targets[aliasLabel]; ok || aliases[aliasLabel] != nil {
			return nil, fmt.Errorf("duplicate target label: %s (package file %s)", alias.Name, pkg.SourceFilePath)
		}

		aliases[aliasLabel] = &model.Alias{
			Label:  aliasLabel,
			Actual: actualLabel,
		}
	}

	return &model.Package{
		SourceFilePath: pkg.SourceFilePath,
		Targets:        targets,
		Aliases:        aliases,
	}, nil
}

// resolveInputs resolves the glob patterns in the inputs and excludeInputs
func resolveInputs(
	logger *zap.SugaredLogger,
	absolutePackagePath string,
	inputs []string,
	excludeInputs []string,
) ([]string, error) {
	var resolvedInputs []string
	fsys := os.DirFS(absolutePackagePath)

	// First, resolve all input patterns
	for _, input := range inputs {
		if !strings.ContainsAny(input, "*?[{") {
			// Nothing to resolve - no special glob characters
			resolvedInputs = append(resolvedInputs, input)
			continue
		}

		matches, err := doublestar.Glob(fsys, input, doublestar.WithFilesOnly())
		if err != nil {
			return nil, fmt.Errorf("failed to resolve glob pattern %s: %w", input, err)
		}

		resolvedInputs = append(resolvedInputs, matches...)
	}

	// If there are no exclusions, return early
	if len(excludeInputs) == 0 {
		return resolvedInputs, nil
	}

	// Resolve exclusion patterns
	var excludedPaths []string
	for _, excludePattern := range excludeInputs {
		matches, err := doublestar.Glob(fsys, excludePattern, doublestar.WithFilesOnly())
		if err != nil {
			return nil, fmt.Errorf("failed to resolve exclusion glob pattern %s: %w", excludePattern, err)
		}
		logger.Debugf("Resolved exclusion glob pattern %s in %s to %v", excludePattern, absolutePackagePath, matches)

		excludedPaths = append(excludedPaths, matches...)
	}

	// Create a map for faster lookup of excluded paths
	excludeMap := make(map[string]bool)
	for _, path := range excludedPaths {
		excludeMap[path] = true
	}

	var filteredInputs []string
	for _, input := range resolvedInputs {
		if !excludeMap[input] {
			filteredInputs = append(filteredInputs, input)
		}
	}

	logger.Debugf("Filtered %d inputs to %d after applying %d exclusions",
		len(resolvedInputs), len(filteredInputs), len(excludedPaths))

	return filteredInputs, nil
}
