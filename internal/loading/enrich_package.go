package loading

import (
	"fmt"
	"os"
	"slices"
	"strings"
	"time"

	"grog/internal/config"
	"grog/internal/console"
	"grog/internal/label"
	"grog/internal/model"
	"grog/internal/output"

	"github.com/bmatcuk/doublestar/v4"
)

// getEnrichedPackage enriches the parsing dto with the following information
// - adds the package path to the target labels
// - resolves the globs in the inputs
// - applies any defaults
// - parses the deps into target labels.
func getEnrichedPackage(logger *console.Logger, packagePath string, pkg PackageDTO) (*model.Package, error) {
	targets := make(map[label.TargetLabel]*model.Target)
	aliases := make(map[label.TargetLabel]*model.Alias)
	absolutePackagePath := config.GetPathAbsoluteToWorkspaceRoot(packagePath)

	// root package is always encoded as ""
	if packagePath == "." {
		packagePath = ""
	}

	for _, target := range pkg.Targets {
		enrichedTarget, err := enrichTarget(logger, packagePath, absolutePackagePath, pkg, target, targets)
		if err != nil {
			return nil, err
		}
		targets[enrichedTarget.Label] = enrichedTarget
	}

	resources, err := enrichResources(packagePath, pkg, targets)
	if err != nil {
		return nil, err
	}

	for _, alias := range pkg.Aliases {
		actualLabel, err := label.ParseTargetLabel(packagePath, alias.Actual)
		if err != nil {
			return nil, err
		}

		aliasLabel := label.TargetLabel{Package: packagePath, Name: alias.Name}
		if _, ok := targets[aliasLabel]; ok || aliases[aliasLabel] != nil || resources[aliasLabel] != nil {
			return nil, fmt.Errorf("duplicate target label: %s (package file %s)", alias.Name, pkg.SourceFilePath)
		}

		aliases[aliasLabel] = &model.Alias{
			SourceFilePath: pkg.SourceFilePath,
			Label:          aliasLabel,
			Actual:         actualLabel,
		}
	}

	dependencyResolvers, err := enrichDependencyResolvers(logger, packagePath, absolutePackagePath, pkg, targets, aliases, resources)
	if err != nil {
		return nil, err
	}

	return &model.Package{
		DependencyResolvers: dependencyResolvers,
		Path:                packagePath,
		Targets:             targets,
		Aliases:             aliases,
		Resources:           resources,
	}, nil
}

// enrichTarget converts a target dto into a model target, rejecting labels already present in targets.
func enrichTarget(
	logger *console.Logger,
	packagePath string,
	absolutePackagePath string,
	pkg PackageDTO,
	target *TargetDTO,
	targets map[label.TargetLabel]*model.Target,
) (*model.Target, error) {
	var deps []label.TargetLabel
	// parse labels
	for _, dep := range target.Dependencies {
		depLabel, err := label.ParseTargetLabel(packagePath, dep)
		if err != nil {
			return nil, err
		}
		deps = append(deps, depLabel)
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
		if !parsedBinOutput.IsFile() {
			return nil, fmt.Errorf("bin output %s for target %s must be of type file",
				target.BinOutput, targetLabel)
		}
	}

	var timeout time.Duration
	if target.Timeout != "" {
		timeout, err = time.ParseDuration(target.Timeout)
		if err != nil {
			return nil, fmt.Errorf("failed to parse timeout for target %s: %w", targetLabel, err)
		}
	}

	// Determine the platforms to use
	// If target has its own platforms, use those
	// Otherwise, use the package default platforms if available
	var targetPlatforms []string
	switch {
	case target.Platforms != nil:
		targetPlatforms = append([]string{}, target.Platforms...)
	case pkg.DefaultPlatforms != nil:
		targetPlatforms = append([]string{}, pkg.DefaultPlatforms...)
	}

	var ociPush map[string][]string
	if len(target.OciPush) > 0 {
		ociPush = make(map[string][]string, len(target.OciPush))
		for local, dst := range target.OciPush {
			ociPush[local] = []string(dst)
		}
	}

	var dependencyResolvers []label.TargetLabel
	for _, resolver := range target.DependencyResolvers {
		resolverLabel, parseError := label.ParseTargetLabel(packagePath, resolver)
		if parseError != nil {
			return nil, fmt.Errorf("failed to parse dependency resolver for target %s: %w", targetLabel, parseError)
		}
		dependencyResolvers = append(dependencyResolvers, resolverLabel)
	}

	return &model.Target{
		DependencyResolvers:  dependencyResolvers,
		SourceFilePath:       pkg.SourceFilePath,
		Label:                targetLabel,
		Command:              target.Command,
		Dependencies:         deps,
		Inputs:               resolvedInputs,
		UnresolvedInputs:     target.Inputs,
		ExcludeInputs:        target.ExcludeInputs,
		Outputs:              parsedOutputs,
		OciPush:              ociPush,
		BinOutput:            parsedBinOutput,
		BinaryRequiresPush:   target.BinaryRequiresPush,
		Platforms:            targetPlatforms,
		OutputChecks:         target.OutputChecks,
		Tags:                 target.Tags,
		Fingerprint:          target.Fingerprint,
		EnvironmentVariables: target.EnvironmentVariables,
		Timeout:              timeout,
		ConcurrencyGroup:     target.ConcurrencyGroup,
	}, nil
}

// enrichResources converts the package's resource dtos into model resources.
func enrichResources(
	packagePath string,
	pkg PackageDTO,
	targets map[label.TargetLabel]*model.Target,
) (map[label.TargetLabel]*model.Resource, error) {
	resources := make(map[label.TargetLabel]*model.Resource)
	for _, resource := range pkg.Resources {
		var resourceDeps []label.TargetLabel
		for _, dep := range resource.Dependencies {
			depLabel, err := label.ParseTargetLabel(packagePath, dep)
			if err != nil {
				return nil, err
			}
			resourceDeps = append(resourceDeps, depLabel)
		}

		resourceLabel := label.TargetLabel{Package: packagePath, Name: resource.Name}
		if _, ok := targets[resourceLabel]; ok || resources[resourceLabel] != nil {
			return nil, fmt.Errorf("duplicate target label: %s (package file %s)", resource.Name, pkg.SourceFilePath)
		}

		if resource.Up == "" {
			return nil, fmt.Errorf("resource %s must define an up command (package file %s)", resourceLabel, pkg.SourceFilePath)
		}

		var resourceTimeout time.Duration
		if resource.Timeout != "" {
			var err error
			resourceTimeout, err = time.ParseDuration(resource.Timeout)
			if err != nil {
				return nil, fmt.Errorf("failed to parse timeout for resource %s: %w", resourceLabel, err)
			}
		}

		resources[resourceLabel] = &model.Resource{
			SourceFilePath: pkg.SourceFilePath,
			Label:          resourceLabel,
			Up:             resource.Up,
			Down:           resource.Down,
			Ready:          resource.Ready,
			Timeout:        resourceTimeout,
			Exports:        resource.Exports,
			Dependencies:   resourceDeps,
		}
	}
	return resources, nil
}

// enrichDependencyResolvers converts the package's dependency resolver dtos into model dependency resolvers.
func enrichDependencyResolvers(
	logger *console.Logger,
	packagePath string,
	absolutePackagePath string,
	pkg PackageDTO,
	targets map[label.TargetLabel]*model.Target,
	aliases map[label.TargetLabel]*model.Alias,
	resources map[label.TargetLabel]*model.Resource,
) (map[label.TargetLabel]*model.DependencyResolver, error) {
	dependencyResolvers := make(map[label.TargetLabel]*model.DependencyResolver)
	for _, resolver := range pkg.DependencyResolvers {
		resolverLabel, enrichmentError := label.ParseTargetLabel(packagePath, ":"+resolver.Name)
		if enrichmentError != nil {
			return nil, fmt.Errorf("invalid dependency resolver name: %w", enrichmentError)
		}
		if targets[resolverLabel] != nil || aliases[resolverLabel] != nil || resources[resolverLabel] != nil || dependencyResolvers[resolverLabel] != nil {
			return nil, fmt.Errorf("duplicate target label: %s (package file %s)", resolver.Name, pkg.SourceFilePath)
		}
		if resolver.Command == "" {
			return nil, fmt.Errorf("dependency resolver %s must define a command (package file %s)", resolverLabel, pkg.SourceFilePath)
		}
		if resolver.Command == "builtin::node" {
			return nil, fmt.Errorf("dependency resolver %s: builtin::node was split; use builtin::pnpm, builtin::npm or builtin::yarn (package file %s)", resolverLabel, pkg.SourceFilePath)
		}
		timeout := 60 * time.Second
		if resolver.Timeout != "" {
			timeout, enrichmentError = time.ParseDuration(resolver.Timeout)
			if enrichmentError != nil {
				return nil, fmt.Errorf("failed to parse timeout for dependency resolver %s: %w", resolverLabel, enrichmentError)
			}
		}
		inputs, excludeInputs := resolver.Inputs, resolver.ExcludeInputs
		if len(inputs) == 0 {
			var defaultExcludeInputs []string
			switch resolver.Command {
			case "builtin::cargo":
				inputs, defaultExcludeInputs = cargoDefaultInputs(absolutePackagePath)
			case "builtin::npm", "builtin::yarn":
				inputs, defaultExcludeInputs = npmPackageManager.defaultInputs(absolutePackagePath)
			case "builtin::pnpm":
				inputs, defaultExcludeInputs = pnpmPackageManager.defaultInputs(absolutePackagePath)
			case "builtin::aube":
				inputs, defaultExcludeInputs = aubePackageManager.defaultInputs(absolutePackagePath)
			case "builtin::uv":
				inputs, defaultExcludeInputs = uvDefaultInputs(absolutePackagePath)
			}
			excludeInputs = slices.Concat(defaultExcludeInputs, excludeInputs)
		}
		resolvedInputs, enrichmentError := resolveInputs(logger, absolutePackagePath, inputs, excludeInputs)
		if enrichmentError != nil {
			return nil, fmt.Errorf("failed to resolve inputs for dependency resolver %s: %w", resolverLabel, enrichmentError)
		}
		synthesizedTarget := resolver.GeneratedTargetName
		if synthesizedTarget == "" {
			synthesizedTarget = "_" + resolver.Name + "_package"
		}
		if _, enrichmentError := label.ParseTargetLabel(packagePath, ":"+synthesizedTarget); enrichmentError != nil {
			return nil, fmt.Errorf("invalid generated_target_name for dependency resolver %s: %w", resolverLabel, enrichmentError)
		}
		dependencyResolvers[resolverLabel] = &model.DependencyResolver{
			SourceFilePath: pkg.SourceFilePath, Label: resolverLabel, Command: resolver.Command, Inputs: resolvedInputs, Timeout: timeout,
			GeneratedTargetName: synthesizedTarget,
		}
	}
	return dependencyResolvers, nil
}

// resolveInputs resolves the glob patterns in the inputs and drops the files matching excludeInputs.
func resolveInputs(
	logger *console.Logger,
	absolutePackagePath string,
	inputs []string,
	excludeInputs []string,
) ([]string, error) {
	for _, excludePattern := range excludeInputs {
		if !doublestar.ValidatePattern(excludePattern) {
			return nil, fmt.Errorf("failed to resolve exclusion glob pattern %s: %w", excludePattern, doublestar.ErrBadPattern)
		}
	}
	fsys := excludingFS{FS: os.DirFS(absolutePackagePath), excludeInputs: excludeInputs}

	var resolvedInputs []string
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

	if len(excludeInputs) == 0 {
		return resolvedInputs, nil
	}

	var filteredInputs []string
	for _, input := range resolvedInputs {
		if !slices.ContainsFunc(excludeInputs, func(pattern string) bool { return doublestar.MatchUnvalidated(pattern, input) }) {
			filteredInputs = append(filteredInputs, input)
		}
	}

	logger.Debugf("Filtered %d inputs to %d after applying %d exclusions",
		len(resolvedInputs), len(filteredInputs), len(excludeInputs))

	return filteredInputs, nil
}
