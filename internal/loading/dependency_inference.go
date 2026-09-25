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

func inferDependencies(loadContext context.Context, packages []*model.Package) ([]*model.Package, error) {
	declarations := make(map[label.TargetLabel]*model.DependencyResolver)
	registrations := make(map[label.TargetLabel]map[string]*model.Target)
	packagesByPath := make(map[string]*model.Package, len(packages))
	var targets []*model.Target
	for _, loadedPackage := range packages {
		maps.Copy(declarations, loadedPackage.DependencyResolvers)
		packagesByPath[loadedPackage.Path] = loadedPackage
		targets = append(targets, loadedPackage.GetTargets()...)
	}
	declaredLabels := make([]string, 0, len(declarations))
	for resolverLabel := range declarations {
		declaredLabels = append(declaredLabels, resolverLabel.String())
	}
	slices.Sort(declaredLabels)
	slices.SortFunc(targets, func(first, second *model.Target) int {
		return strings.Compare(first.Label.String(), second.Label.String())
	})
	for _, target := range targets {
		targetLabel := target.Label
		for _, resolverLabel := range target.DependencyResolvers {
			if declarations[resolverLabel] == nil {
				return nil, fmt.Errorf("no dependency resolver at %s (referenced by %s); declared: %s", resolverLabel, targetLabel, strings.Join(declaredLabels, ", "))
			}
			if registrations[resolverLabel] == nil {
				registrations[resolverLabel] = make(map[string]*model.Target)
			}
			if previous := registrations[resolverLabel][path.Join(targetLabel.Package)]; previous != nil && previous.Label != targetLabel {
				return nil, fmt.Errorf("resolver %s registered twice in package %s: %s and %s", resolverLabel, targetLabel.Package, previous.Label, targetLabel)
			}
			registrations[resolverLabel][path.Join(targetLabel.Package)] = target
		}
	}
	// Every declared resolver runs: a package it synthesizes for may register nothing.
	resolvers := slices.SortedFunc(maps.Values(declarations), func(first, second *model.DependencyResolver) int {
		return strings.Compare(first.Label.String(), second.Label.String())
	})
	documents := make([]resolverDocument, len(resolvers))
	resolverGroup, resolverContext := errgroup.WithContext(loadContext)
	for index, resolver := range resolvers {
		resolverGroup.Go(func() error {
			document, operationError := runDependencyResolver(resolverContext, resolver)
			if operationError != nil {
				return operationError
			}
			documents[index] = document
			return nil
		})
	}
	if operationError := resolverGroup.Wait(); operationError != nil {
		return nil, operationError
	}
	// Endpoints first, edges second: a dependency may itself be synthesized.
	for index, resolver := range resolvers {
		resolverLabel := resolver.Label
		document := documents[index]
		for _, packagePath := range slices.Sorted(maps.Keys(document.Packages)) {
			fullPath := path.Join(resolverLabel.Package, packagePath)
			if registrations[resolverLabel][fullPath] != nil || len(document.Packages[packagePath].Inputs) == 0 {
				continue
			}
			synthesized, createdPackage, operationError := synthesizeFilegroup(loadContext, resolver, fullPath, document.Packages[packagePath].Inputs, packagesByPath)
			if operationError != nil {
				return nil, operationError
			}
			if createdPackage != nil {
				packages = append(packages, createdPackage)
			}
			if registrations[resolverLabel] == nil {
				registrations[resolverLabel] = make(map[string]*model.Target)
			}
			registrations[resolverLabel][fullPath] = synthesized
		}
	}
	for index, resolver := range resolvers {
		resolverLabel := resolver.Label
		document := documents[index]
		for _, packagePath := range slices.Sorted(maps.Keys(document.Packages)) {
			target := registrations[resolverLabel][path.Join(resolverLabel.Package, packagePath)]
			if target == nil {
				console.GetLogger(loadContext).Debugf("resolver %s reports unregistered package %s; ignoring", resolverLabel, packagePath)
				continue
			}
			for _, dependency := range document.Packages[packagePath].Dependencies {
				var dependencyLabel label.TargetLabel
				if strings.HasPrefix(dependency, "//") {
					parsedLabel, operationError := label.ParseTargetLabel("", dependency)
					if operationError != nil {
						return nil, fmt.Errorf("resolver %s reports invalid dependency %q: %w", resolverLabel, dependency, operationError)
					}
					dependencyLabel = parsedLabel
				} else {
					dependencyTarget := registrations[resolverLabel][path.Join(resolverLabel.Package, dependency)]
					if dependencyTarget == nil {
						return nil, fmt.Errorf("resolver %s reports %s depends on %s, which has no target registered for %s and no inputs to synthesize one from", resolverLabel, packagePath, dependency, resolverLabel)
					}
					dependencyLabel = dependencyTarget.Label
				}
				if dependencyLabel != target.Label {
					target.Dependencies = append(target.Dependencies, dependencyLabel)
				}
			}
		}
	}
	for _, resolverTargets := range registrations {
		for _, target := range resolverTargets {
			slices.SortFunc(target.Dependencies, func(first, second label.TargetLabel) int { return strings.Compare(first.String(), second.String()) })
			target.Dependencies = slices.Compact(target.Dependencies)
		}
	}
	return packages, loadContext.Err()
}

// synthesizeFilegroup gives a package that registers no target a filegroup
// built from the inputs its resolver declared. The resolver's BUILD file is
// recorded as the defining file so `grog changes` and duplicate-label errors
// have one to point at. The second result is the package it had to create
// because no BUILD file exists there, or nil.
func synthesizeFilegroup(loadContext context.Context, resolver *model.DependencyResolver, packagePath string, inputs []string, packagesByPath map[string]*model.Package) (*model.Target, *model.Package, error) {
	targetLabel := label.TargetLabel{Package: packagePath, Name: "_" + resolver.Label.Name + "_package"}
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

func runDependencyResolver(loadContext context.Context, resolver *model.DependencyResolver) (resolverDocument, error) {
	resolverContext, cancel := context.WithTimeout(loadContext, resolver.Timeout)
	defer cancel()
	logger := console.GetLogger(loadContext)
	logger.Debugf("resolver %s: %s", resolver.Label, resolver.Command)
	var document resolverDocument
	if resolver.Command == "builtin:cargo" {
		var operationError error
		document, operationError = cargoDependencies(resolverContext, config.GetPathAbsoluteToWorkspaceRoot(resolver.Label.Package))
		if operationError != nil {
			return document, fmt.Errorf("resolver %s failed: %w", resolver.Label, operationError)
		}
	} else {
		command, cleanup, operationError := shell.NewCommand(resolverContext, shell.WithDefaultFlags(resolver.Command))
		if operationError != nil {
			return document, fmt.Errorf("resolver %s failed: %w", resolver.Label, operationError)
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
				return document, fmt.Errorf("resolver %s timed out after %s: %w\n%s", resolver.Label, resolver.Timeout, resolverContext.Err(), standardError.String())
			}
			if resolverContext.Err() != nil {
				return document, fmt.Errorf("resolver %s failed: %w\n%s", resolver.Label, resolverContext.Err(), standardError.String())
			}
			return document, fmt.Errorf("resolver %s failed: %w\n%s", resolver.Label, operationError, standardError.String())
		}
		logger.Debugf("resolver %s stderr: %s", resolver.Label, standardError.String())
		if operationError := json.Unmarshal(standardOutput.Bytes(), &document); operationError != nil {
			return document, fmt.Errorf("resolver %s returned invalid JSON: %w; stdout: %q", resolver.Label, operationError, standardOutput.Bytes()[:min(standardOutput.Len(), 2048)])
		}
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
		mapping, operationError := json.Marshal(document)
		if operationError != nil {
			return document, fmt.Errorf("encode resolver %s mapping: %w", resolver.Label, operationError)
		}
		logger.Debugf("resolver %s mapping: %s", resolver.Label, mapping)
	}
	return document, nil
}

func validateResolverPath(entry string) error {
	if strings.HasPrefix(entry, "/") || strings.HasPrefix(entry, "./") || strings.HasSuffix(entry, "/") || strings.Contains(entry, "\\") || (len(entry) > 1 && entry[1] == ':') || slices.Contains(strings.Split(entry, "/"), "..") {
		return fmt.Errorf("invalid path %q: paths must be relative, slash-separated, without a leading ./, trailing /, or .. segment", entry)
	}
	return nil
}
