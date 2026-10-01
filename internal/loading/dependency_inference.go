package loading

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"

	"grog/internal/caching"
	"grog/internal/caching/backends"
	"grog/internal/config"
	"grog/internal/console"
	"grog/internal/hashing"
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
	"builtin::uv":    uvDependencies,
}

// GrogVersion keys the cached output of built-in resolvers; cmd.Stamp sets it.
var GrogVersion string

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

	var cas *caching.Cas
	if len(resolvers) > 0 && config.Global.EnableCache {
		backend, operationError := backends.GetCacheBackend(loadContext, config.Global.Cache)
		if operationError != nil {
			return nil, fmt.Errorf("could not instantiate cache for dependency resolvers: %w", operationError)
		}
		cas = caching.NewCas(backend)
	}
	documents := make([]resolverDocument, len(resolvers))
	resolverGroup, resolverContext := errgroup.WithContext(loadContext)
	for index, resolver := range resolvers {
		resolverGroup.Go(func() error {
			var operationError error
			documents[index], operationError = runDependencyResolver(resolverContext, resolver, cas)
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
	if operationError := linkDependencies(loadContext, resolvers, documents, registrations); operationError != nil {
		return nil, operationError
	}
	return packages, nil
}

// linkDependencies appends every reported edge to the target registered for
// the reported package. Path entries resolve through the registrations, label
// entries are parsed as they are.
func linkDependencies(loadContext context.Context, resolvers []*model.DependencyResolver, documents []resolverDocument, registrations map[registration]*model.Target) error {
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
						return fmt.Errorf("resolver %s reports invalid dependency %q: %w", resolver.Label, dependency, operationError)
					}
					dependencyLabel = parsedLabel
				} else {
					dependencyTarget := registrations[registration{resolver.Label, path.Join(resolver.Label.Package, dependency)}]
					if dependencyTarget == nil {
						return fmt.Errorf("resolver %s reports %s depends on %s, which has no target registered for %s and no inputs to synthesize one from", resolver.Label, packagePath, dependency, resolver.Label)
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
	return nil
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

// runDependencyResolver produces one resolver's document and checks it
// against version 1 of the protocol. The document comes from the cache when
// the resolver's inputs are unchanged, from Go for a builtin:: command and
// otherwise from the JSON its shell command prints. Only valid output is cached.
func runDependencyResolver(loadContext context.Context, resolver *model.DependencyResolver, cas *caching.Cas) (resolverDocument, error) {
	resolverContext, cancel := context.WithTimeout(loadContext, resolver.Timeout)
	defer cancel()
	logger := console.GetLogger(loadContext)
	var document resolverDocument
	cacheKey, operationError := resolverCacheKey(resolver)
	if operationError != nil {
		return document, fmt.Errorf("resolver %s failed: %w", resolver.Label, operationError)
	}
	logger.Debugf("resolver %s: %s (cache key %s)", resolver.Label, resolver.Command, cacheKey)
	var output []byte
	cacheHit := false
	if cas != nil {
		output, operationError = cas.LoadBytes(resolverContext, cacheKey)
		cacheHit = operationError == nil
	}
	if !cacheHit {
		if builtin, isBuiltin := builtinResolvers[resolver.Command]; isBuiltin {
			document, operationError = builtin(resolverContext, config.GetPathAbsoluteToWorkspaceRoot(resolver.Label.Package))
			if operationError == nil {
				output, operationError = json.Marshal(document)
			}
		} else {
			output, operationError = runResolverCommand(resolverContext, resolver)
		}
		if operationError != nil {
			return document, fmt.Errorf("resolver %s failed: %w", resolver.Label, operationError)
		}
	}
	if operationError := json.Unmarshal(output, &document); operationError != nil {
		return document, fmt.Errorf("resolver %s returned invalid JSON: %w; stdout: %q", resolver.Label, operationError, output[:min(len(output), 2048)])
	}
	edgeCount, operationError := validateResolverDocument(resolver.Label, document)
	if operationError != nil {
		return document, operationError
	}
	if cas != nil && !cacheHit {
		if operationError := cas.WriteBytes(resolverContext, cacheKey, output); operationError != nil {
			return document, fmt.Errorf("resolver %s: failed to cache output: %w", resolver.Label, operationError)
		}
	}
	cacheStatus := "cache miss"
	if cacheHit {
		cacheStatus = "cache hit"
	}
	logger.Debugf("resolver %s: %s, %s, %d packages, %d edges", resolver.Label, resolver.Command, cacheStatus, len(document.Packages), edgeCount)
	if logger.DebugEnabled() {
		logger.Debugf("resolver %s mapping: %s", resolver.Label, output)
	}
	return document, nil
}

// validateResolverDocument checks a document against version 1 of the
// protocol and returns how many edges it reports. Dependencies that are
// labels are parsed when they are linked, so only path entries are checked.
func validateResolverDocument(resolverLabel label.TargetLabel, document resolverDocument) (int, error) {
	if document.Version > 1 {
		return 0, fmt.Errorf("resolver %s returned unsupported version %d; upgrade grog", resolverLabel, document.Version)
	}
	if document.Version != 1 {
		return 0, fmt.Errorf("resolver %s must return version 1", resolverLabel)
	}
	if document.Packages == nil {
		return 0, fmt.Errorf("resolver %s must return a packages object", resolverLabel)
	}

	edgeCount := 0
	for _, packagePath := range slices.Sorted(maps.Keys(document.Packages)) {
		if operationError := validateResolverPath(packagePath); operationError != nil {
			return 0, fmt.Errorf("resolver %s: %w", resolverLabel, operationError)
		}

		reportedPackage := document.Packages[packagePath]
		slices.Sort(reportedPackage.Dependencies)
		edgeCount += len(reportedPackage.Dependencies)
		for _, dependency := range reportedPackage.Dependencies {
			if strings.HasPrefix(dependency, "//") {
				continue
			}
			if operationError := validateResolverPath(dependency); operationError != nil {
				return 0, fmt.Errorf("resolver %s: %w", resolverLabel, operationError)
			}
		}

		for _, input := range reportedPackage.Inputs {
			if operationError := validateResolverPath(input); operationError != nil {
				return 0, fmt.Errorf("resolver %s: inputs of %s: %w", resolverLabel, packagePath, operationError)
			}
		}
	}
	return edgeCount, nil
}

// resolverCacheKey hashes everything the output depends on: the protocol
// version, the label and command, grog's version for a built-in or the command
// environment for a shell resolver, and the path, size and contents of every
// resolved input. Inputs that do not exist are skipped.
func resolverCacheKey(resolver *model.DependencyResolver) (string, error) {
	// Every field ends in NUL and every file carries its size, so two keys
	// only collide when their fields are identical: "ab"+"c" and "a"+"bc"
	// hash differently, and so do a file "b" and a deleted file next to it.
	hasher := hashing.GetHasher()
	_, _ = fmt.Fprintf(hasher, "1\x00%s\x00%s\x00", resolver.Label, resolver.Command)

	if strings.HasPrefix(resolver.Command, "builtin::") {
		_, _ = fmt.Fprintf(hasher, "%s\x00", GrogVersion)
	} else {
		// Checkout-specific values would defeat the cache, and the protocol
		// forbids output that depends on them.
		environment := LoaderEnv()
		delete(environment, "GROG_GIT_HASH")
		delete(environment, "GROG_WORKSPACE_ROOT")
		maps.Copy(environment, config.Global.EnvironmentVariables)
		for _, name := range slices.Sorted(maps.Keys(environment)) {
			_, _ = fmt.Fprintf(hasher, "%s=%s\x00", name, environment[name])
		}
		_, _ = fmt.Fprintf(hasher, "%t\x00", config.Global.DisableDefaultShellFlags)
	}

	packageDirectory := config.GetPathAbsoluteToWorkspaceRoot(resolver.Label.Package)
	for _, input := range slices.Sorted(slices.Values(resolver.Inputs)) {
		file, operationError := os.Open(filepath.Join(packageDirectory, input))
		if errors.Is(operationError, os.ErrNotExist) {
			continue
		}
		if operationError != nil {
			return "", operationError
		}

		fileInfo, operationError := file.Stat()
		if operationError == nil {
			_, _ = fmt.Fprintf(hasher, "%s\x00%d\x00", input, fileInfo.Size())
			_, operationError = io.Copy(hasher, file)
		}
		_ = file.Close()
		if operationError != nil {
			return "", operationError
		}
	}

	return hasher.SumString(), nil
}

// runResolverCommand runs the resolver's shell command in its package with a
// target's environment and returns what it printed to stdout.
func runResolverCommand(resolverContext context.Context, resolver *model.DependencyResolver) ([]byte, error) {
	command, cleanup, operationError := shell.NewCommand(resolverContext, shell.WithDefaultFlags(resolver.Command))
	if operationError != nil {
		return nil, operationError
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
			return nil, fmt.Errorf("timed out after %s\n%s", resolver.Timeout, standardError.String())
		}
		return nil, fmt.Errorf("%w\n%s", operationError, standardError.String())
	}
	console.GetLogger(resolverContext).Debugf("resolver %s stderr: %s", resolver.Label, standardError.String())
	return standardOutput.Bytes(), nil
}

func validateResolverPath(entry string) error {
	if strings.HasPrefix(entry, "/") || strings.HasPrefix(entry, "./") || strings.HasSuffix(entry, "/") || strings.Contains(entry, "\\") || (len(entry) > 1 && entry[1] == ':') || slices.Contains(strings.Split(entry, "/"), "..") {
		return fmt.Errorf("invalid path %q: paths must be relative, slash-separated, without a leading ./, trailing /, or .. segment", entry)
	}
	return nil
}
