package loading

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"os"
	"path"
	"slices"
	"strings"

	"grog/internal/config"
	"grog/internal/console"
	"grog/internal/label"
	"grog/internal/model"
	"grog/internal/shell"

	"golang.org/x/sync/errgroup"
)

type resolverDocument struct {
	Version  int                        `json:"version"`
	Packages map[string]resolverPackage `json:"packages"`
}

type resolverPackage struct {
	Dependencies []string `json:"dependencies"`
	// Inputs let the resolver stand in for a package that registers no target:
	// grog synthesizes a filegroup from them. Ignored when a target registers.
	Inputs []string `json:"inputs"`
}

// UnmarshalJSON requires each package entry to be an object.
func (reportedPackage *resolverPackage) UnmarshalJSON(contents []byte) error {
	if bytes.Equal(bytes.TrimSpace(contents), []byte("null")) {
		return fmt.Errorf("package must be an object")
	}
	type packageFields resolverPackage
	return json.Unmarshal(contents, (*packageFields)(reportedPackage))
}

var builtinResolvers = map[string]func(context.Context, string) (resolverDocument, error){
	"builtin::cargo": cargoDependencies,
}

// inferDependencies runs every declared dependency resolver and appends the
// edges it reports to the target each package registered for it. A reported
// package that registers nothing gets a filegroup synthesized from its inputs.
func inferDependencies(loadContext context.Context, packages []*model.Package) ([]*model.Package, error) {
	declarations := make(map[label.TargetLabel]*model.DependencyResolver)
	packagesByPath := make(map[string]*model.Package, len(packages))
	var targets []*model.Target
	for _, loadedPackage := range packages {
		maps.Copy(declarations, loadedPackage.DependencyResolvers)
		packagesByPath[loadedPackage.Path] = loadedPackage
		targets = append(targets, loadedPackage.GetTargets()...)
	}
	resolvers := slices.SortedFunc(maps.Values(declarations), func(first, second *model.DependencyResolver) int {
		return strings.Compare(first.Label.String(), second.Label.String())
	})
	registrations, operationError := registerTargets(resolvers, targets)
	if operationError != nil {
		return nil, operationError
	}

	documents := make([]resolverDocument, len(resolvers))
	resolverGroup, resolverContext := errgroup.WithContext(loadContext)
	for index, resolver := range resolvers {
		resolverGroup.Go(func() error {
			var operationError error
			documents[index], operationError = runDependencyResolver(resolverContext, resolver)
			return operationError
		})
	}
	if operationError := resolverGroup.Wait(); operationError != nil {
		return nil, operationError
	}

	// Synthesize before linking: a reported dependency may itself be synthesized.
	for index, resolver := range resolvers {
		document := documents[index]
		for _, packagePath := range slices.Sorted(maps.Keys(document.Packages)) {
			key := registration{resolver.Label, path.Join(resolver.Label.Package, packagePath)}
			inputs := document.Packages[packagePath].Inputs
			if registrations[key] != nil || len(inputs) == 0 {
				continue
			}
			synthesized, createdPackage, operationError := synthesizeFilegroup(loadContext, resolver, key.packagePath, inputs, packagesByPath)
			if operationError != nil {
				return nil, operationError
			}
			if createdPackage != nil {
				packages = append(packages, createdPackage)
			}
			registrations[key] = synthesized
		}
	}
	for index, resolver := range resolvers {
		document := documents[index]
		for _, packagePath := range slices.Sorted(maps.Keys(document.Packages)) {
			target := registrations[registration{resolver.Label, path.Join(resolver.Label.Package, packagePath)}]
			if target == nil {
				console.GetLogger(loadContext).Debugf("resolver %s reports unregistered package %s; ignoring", resolver.Label, packagePath)
				continue
			}
			for _, dependency := range document.Packages[packagePath].Dependencies {
				var dependencyLabel label.TargetLabel
				if strings.HasPrefix(dependency, "//") {
					parsedLabel, operationError := label.ParseTargetLabel("", dependency)
					if operationError != nil {
						return nil, fmt.Errorf("resolver %s reports invalid dependency %q: %w", resolver.Label, dependency, operationError)
					}
					dependencyLabel = parsedLabel
				} else {
					dependencyTarget := registrations[registration{resolver.Label, path.Join(resolver.Label.Package, dependency)}]
					if dependencyTarget == nil {
						return nil, fmt.Errorf("resolver %s reports %s depends on %s, which has no target registered for %s and no inputs to synthesize one from", resolver.Label, packagePath, dependency, resolver.Label)
					}
					dependencyLabel = dependencyTarget.Label
				}
				if dependencyLabel != target.Label {
					target.Dependencies = append(target.Dependencies, dependencyLabel)
				}
			}
			slices.SortFunc(target.Dependencies, func(first, second label.TargetLabel) int { return strings.Compare(first.String(), second.String()) })
			target.Dependencies = slices.Compact(target.Dependencies)
		}
	}
	return packages, nil
}

type registration struct {
	resolver    label.TargetLabel
	packagePath string
}

// registerTargets finds the one target per package that lists each resolver
// in dependency_resolvers.
func registerTargets(resolvers []*model.DependencyResolver, targets []*model.Target) (map[registration]*model.Target, error) {
	declared := make(map[label.TargetLabel]bool, len(resolvers))
	declaredLabels := make([]string, 0, len(resolvers))
	for _, resolver := range resolvers {
		declared[resolver.Label] = true
		declaredLabels = append(declaredLabels, resolver.Label.String())
	}
	registrations := make(map[registration]*model.Target)
	slices.SortFunc(targets, func(first, second *model.Target) int {
		return strings.Compare(first.Label.String(), second.Label.String())
	})
	for _, target := range targets {
		for _, resolverLabel := range target.DependencyResolvers {
			if !declared[resolverLabel] {
				return nil, fmt.Errorf("no dependency resolver at %s (referenced by %s); declared: %s", resolverLabel, target.Label, strings.Join(declaredLabels, ", "))
			}
			key := registration{resolverLabel, target.Label.Package}
			if previous := registrations[key]; previous != nil && previous != target {
				return nil, fmt.Errorf("resolver %s registered twice in package %s: %s and %s", resolverLabel, target.Label.Package, previous.Label, target.Label)
			}
			registrations[key] = target
		}
	}
	return registrations, nil
}

// synthesizeFilegroup gives a package that registers no target a filegroup
// built from the inputs its resolver declared. The resolver's BUILD file is
// recorded as the defining file so `grog changes` and duplicate-label errors
// have one to point at. The second result is the package it had to create
// because no BUILD file exists there, or nil.
func synthesizeFilegroup(loadContext context.Context, resolver *model.DependencyResolver, packagePath string, inputs []string, packagesByPath map[string]*model.Package) (*model.Target, *model.Package, error) {
	targetLabel := label.TargetLabel{Package: packagePath, Name: resolver.GeneratedTargetName}
	var createdPackage *model.Package
	owningPackage, exists := packagesByPath[packagePath]
	if !exists {
		createdPackage = &model.Package{
			Path:      packagePath,
			Targets:   make(map[label.TargetLabel]*model.Target),
			Aliases:   make(map[label.TargetLabel]*model.Alias),
			Resources: make(map[label.TargetLabel]*model.Resource),
		}
		packagesByPath[packagePath] = createdPackage
		owningPackage = createdPackage
	}
	if existing := owningPackage.Targets[targetLabel]; existing != nil {
		return nil, nil, fmt.Errorf("resolver %s cannot synthesize %s: a target with that name is defined in %s", resolver.Label, targetLabel, existing.SourceFilePath)
	}
	if existing := owningPackage.Aliases[targetLabel]; existing != nil {
		return nil, nil, fmt.Errorf("resolver %s cannot synthesize %s: an alias with that name is defined in %s", resolver.Label, targetLabel, existing.SourceFilePath)
	}
	if existing := owningPackage.Resources[targetLabel]; existing != nil {
		return nil, nil, fmt.Errorf("resolver %s cannot synthesize %s: a resource with that name is defined in %s", resolver.Label, targetLabel, existing.SourceFilePath)
	}
	resolvedInputs, operationError := resolveInputs(console.GetLogger(loadContext), config.GetPathAbsoluteToWorkspaceRoot(packagePath), inputs, nil)
	if operationError != nil {
		return nil, nil, fmt.Errorf("resolver %s: failed to resolve inputs for %s: %w", resolver.Label, targetLabel, operationError)
	}
	target := &model.Target{
		SourceFilePath:      resolver.SourceFilePath,
		Label:               targetLabel,
		Inputs:              resolvedInputs,
		UnresolvedInputs:    inputs,
		DependencyResolvers: []label.TargetLabel{resolver.Label},
	}
	owningPackage.Targets[targetLabel] = target
	return target, createdPackage, nil
}

// runDependencyResolver produces one resolver's document, from Go for a
// builtin:: command and otherwise from the JSON its shell command prints, and
// checks it against version 1 of the protocol.
func runDependencyResolver(loadContext context.Context, resolver *model.DependencyResolver) (resolverDocument, error) {
	resolverContext, cancel := context.WithTimeout(loadContext, resolver.Timeout)
	defer cancel()
	logger := console.GetLogger(loadContext)
	logger.Debugf("resolver %s: %s", resolver.Label, resolver.Command)
	var document resolverDocument
	var operationError error
	if builtin, isBuiltin := builtinResolvers[resolver.Command]; isBuiltin {
		document, operationError = builtin(resolverContext, config.GetPathAbsoluteToWorkspaceRoot(resolver.Label.Package))
	} else {
		document, operationError = runResolverCommand(resolverContext, resolver)
	}
	if operationError != nil {
		return document, fmt.Errorf("resolver %s failed: %w", resolver.Label, operationError)
	}
	if document.Version > 1 {
		return document, fmt.Errorf("resolver %s returned unsupported version %d; upgrade grog", resolver.Label, document.Version)
	}
	if document.Version != 1 {
		return document, fmt.Errorf("resolver %s must return version 1", resolver.Label)
	}
	if document.Packages == nil {
		return document, fmt.Errorf("resolver %s must return a packages object", resolver.Label)
	}
	for _, packagePath := range slices.Sorted(maps.Keys(document.Packages)) {
		if operationError := validateResolverPath(packagePath); operationError != nil {
			return document, fmt.Errorf("resolver %s: %w", resolver.Label, operationError)
		}
		reportedPackage := document.Packages[packagePath]
		slices.Sort(reportedPackage.Dependencies)
		for _, dependency := range reportedPackage.Dependencies {
			if strings.HasPrefix(dependency, "//") {
				continue
			}
			if operationError := validateResolverPath(dependency); operationError != nil {
				return document, fmt.Errorf("resolver %s: %w", resolver.Label, operationError)
			}
		}
		for _, input := range reportedPackage.Inputs {
			if operationError := validateResolverPath(input); operationError != nil {
				return document, fmt.Errorf("resolver %s: inputs of %s: %w", resolver.Label, packagePath, operationError)
			}
		}
	}
	if logger.DebugEnabled() {
		mapping, _ := json.Marshal(document)
		logger.Debugf("resolver %s mapping: %s", resolver.Label, mapping)
	}
	return document, nil
}

// runResolverCommand runs the resolver's shell command in its package with a
// target's environment and parses the JSON it prints to stdout.
func runResolverCommand(resolverContext context.Context, resolver *model.DependencyResolver) (resolverDocument, error) {
	var document resolverDocument
	command, cleanup, operationError := shell.NewCommand(resolverContext, shell.WithDefaultFlags(resolver.Command))
	if operationError != nil {
		return document, operationError
	}
	defer cleanup()
	command.Dir = config.GetPathAbsoluteToWorkspaceRoot(resolver.Label.Package)
	command.Env = os.Environ()
	for name, value := range config.Global.EnvironmentVariables {
		command.Env = append(command.Env, name+"="+value)
	}
	for name, value := range LoaderEnv() {
		command.Env = append(command.Env, name+"="+value)
	}
	command.Env = append(command.Env, "GROG_RESOLVER_LABEL="+resolver.Label.String(), "GROG_TARGET="+resolver.Label.String(), "GROG_PACKAGE="+resolver.Label.Package)
	var standardOutput, standardError bytes.Buffer
	command.Stdout = &standardOutput
	command.Stderr = &standardError
	if operationError := command.Run(); operationError != nil {
		if errors.Is(resolverContext.Err(), context.DeadlineExceeded) {
			return document, fmt.Errorf("timed out after %s\n%s", resolver.Timeout, standardError.String())
		}
		return document, fmt.Errorf("%w\n%s", operationError, standardError.String())
	}
	console.GetLogger(resolverContext).Debugf("resolver %s stderr: %s", resolver.Label, standardError.String())
	if operationError := json.Unmarshal(standardOutput.Bytes(), &document); operationError != nil {
		return document, fmt.Errorf("returned invalid JSON: %w; stdout: %q", operationError, standardOutput.Bytes()[:min(standardOutput.Len(), 2048)])
	}
	return document, nil
}

func validateResolverPath(entry string) error {
	if strings.HasPrefix(entry, "/") || strings.HasPrefix(entry, "./") || strings.HasSuffix(entry, "/") || strings.Contains(entry, "\\") || (len(entry) > 1 && entry[1] == ':') || slices.Contains(strings.Split(entry, "/"), "..") {
		return fmt.Errorf("invalid path %q: paths must be relative, slash-separated, without a leading ./, trailing /, or .. segment", entry)
	}
	return nil
}
