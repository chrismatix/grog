package loading

import (
	"context"
	"fmt"
	"grog/internal/config"
	"grog/internal/model"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"go.starlark.net/lib/json"
	"go.starlark.net/lib/math"
	"go.starlark.net/lib/time"
	"go.starlark.net/starlark"
	"go.starlark.net/starlarkstruct"
	"go.starlark.net/syntax"
)

// StarlarkLoader implements the Loader interface for Starlark files.
type StarlarkLoader struct{}

var starlarkSourceExtensions = []string{".star", ".bzl"}

func (StarlarkLoader) Matches(fileName string) bool {
	return IsStarlarkPackageFile(fileName)
}

// IsStarlarkSourceFile reports whether tooling should treat a file as Starlark.
func IsStarlarkSourceFile(fileName string) bool {
	if IsStarlarkPackageFile(fileName) {
		return true
	}
	for _, extension := range starlarkSourceExtensions {
		if strings.HasSuffix(fileName, extension) {
			return true
		}
	}
	return false
}

// StarlarkSourceExtensions returns file extensions supported by Starlark tooling.
func StarlarkSourceExtensions() []string {
	return slices.Clone(starlarkSourceExtensions)
}

// ResolveStarlarkModulePath resolves a load path using loader semantics.
func ResolveStarlarkModulePath(workspaceRoot string, currentPath string, module string) string {
	if strings.HasPrefix(module, "//") {
		return filepath.Clean(filepath.Join(workspaceRoot, strings.TrimPrefix(module, "//")))
	}
	if filepath.IsAbs(module) {
		return filepath.Clean(module)
	}
	return filepath.Clean(filepath.Join(filepath.Dir(currentPath), module))
}

// StarlarkDeclaration identifies a declaration produced during evaluation.
type StarlarkDeclaration struct {
	Kind   string
	Name   string
	Path   string
	Line   int
	Column int
}

// StarlarkEvaluationOptions configures source and environment resolution.
type StarlarkEvaluationOptions struct {
	WorkspaceRoot       string
	Environment         map[string]string
	PlatformTags        []string
	ReadFile            func(path string) ([]byte, error)
	DeclarationCallback func(StarlarkDeclaration)
}

// starlarkPackageCollector holds the collected targets, aliases, resources, and environments.
type starlarkPackageCollector struct {
	targets          []*TargetDTO
	aliases          []*AliasDTO
	resources        []*ResourceDTO
	environments     []*EnvironmentDTO
	defaultPlatforms []string
	declaration      func(StarlarkDeclaration)
}

// moduleLoadContext tracks loaded modules and in-progress loads for cycle detection.
type moduleLoadContext struct {
	// cache stores already-loaded modules
	cache map[string]starlark.StringDict
	// loading tracks modules currently being loaded to detect cycles
	loading map[string]bool
}

// Load reads the file at the specified filePath and evaluates it as Starlark code.
func (loader StarlarkLoader) Load(ctx context.Context, filePath string) (PackageDTO, bool, error) {
	source, operationError := os.ReadFile(filePath)
	if operationError != nil {
		return PackageDTO{}, false, operationError
	}
	return loader.loadSource(ctx, filePath, source)
}

func (StarlarkLoader) loadSource(_ context.Context, filePath string, source []byte) (PackageDTO, bool, error) {
	environment := LoaderEnv()
	for key, value := range config.Global.EnvironmentVariables {
		environment[key] = value
	}
	packageDTO, operationError := EvaluateStarlark(filePath, source, StarlarkEvaluationOptions{WorkspaceRoot: config.Global.WorkspaceRoot, Environment: environment, PlatformTags: config.Global.PlatformTags})
	if operationError != nil {
		return PackageDTO{}, false, fmt.Errorf("failed to evaluate Starlark file %s: %w", filePath, operationError)
	}
	return packageDTO, true, nil
}

// EvaluateStarlark evaluates source with the same builtins as StarlarkLoader.
func EvaluateStarlark(filePath string, source []byte, options StarlarkEvaluationOptions) (PackageDTO, error) {
	if options.ReadFile == nil {
		options.ReadFile = os.ReadFile
	}
	collector := &starlarkPackageCollector{
		targets:      make([]*TargetDTO, 0),
		aliases:      make([]*AliasDTO, 0),
		resources:    make([]*ResourceDTO, 0),
		environments: make([]*EnvironmentDTO, 0),
		declaration:  options.DeclarationCallback,
	}
	evaluator := &starlarkEvaluator{
		collector: collector,
		options:   options,
		modules:   moduleLoadContext{cache: make(map[string]starlark.StringDict), loading: make(map[string]bool)},
	}
	thread := &starlark.Thread{Name: filePath, Load: evaluator.loadModule}
	_, operationError := starlark.ExecFileOptions(&syntax.FileOptions{}, thread, filePath, source, evaluator.predeclared())
	if operationError != nil {
		return PackageDTO{}, operationError
	}
	return PackageDTO{
		Targets:          collector.targets,
		Aliases:          collector.aliases,
		Resources:        collector.resources,
		Environments:     collector.environments,
		DefaultPlatforms: collector.defaultPlatforms,
	}, nil
}

type starlarkEvaluator struct {
	collector *starlarkPackageCollector
	options   StarlarkEvaluationOptions
	modules   moduleLoadContext
}

func (evaluator *starlarkEvaluator) predeclared() starlark.StringDict {
	predeclared := starlark.StringDict{
		targetDeclarationKind:      starlark.NewBuiltin(targetDeclarationKind, evaluator.collector.targetBuiltin),
		aliasDeclarationKind:       starlark.NewBuiltin(aliasDeclarationKind, evaluator.collector.aliasBuiltin),
		resourceDeclarationKind:    starlark.NewBuiltin(resourceDeclarationKind, evaluator.collector.resourceBuiltin),
		environmentDeclarationKind: starlark.NewBuiltin(environmentDeclarationKind, evaluator.collector.environmentBuiltin),
		"json":                     json.Module,
		"math":                     math.Module,
		"time":                     time.Module,
	}
	for name, value := range evaluator.options.Environment {
		predeclared[name] = starlark.String(value)
	}
	values := make([]starlark.Value, 0, len(evaluator.options.PlatformTags))
	for _, tag := range evaluator.options.PlatformTags {
		values = append(values, starlark.String(tag))
	}
	platformTags := starlark.NewList(values)
	platformTags.Freeze()
	predeclared["GROG_PLATFORM_TAGS"] = platformTags
	return predeclared
}

// loadModule implements the load() function for importing other Starlark files
// with caching and cycle detection.
func (evaluator *starlarkEvaluator) loadModule(thread *starlark.Thread, module string) (starlark.StringDict, error) {
	modulePath := ResolveStarlarkModulePath(evaluator.options.WorkspaceRoot, thread.Name, module)

	// Check cache first - if already loaded, return cached result
	if cached, isLoaded := evaluator.modules.cache[modulePath]; isLoaded {
		return cached, nil
	}

	// Check if currently being loaded - this indicates a cycle
	if evaluator.modules.loading[modulePath] {
		return nil, fmt.Errorf("cycle detected: module %s is already being loaded", module)
	}

	// Check if file exists
	source, operationError := evaluator.options.ReadFile(modulePath)
	if operationError != nil {
		return nil, fmt.Errorf("module not found: %s (resolved to %s): %w", module, modulePath, operationError)
	}
	evaluator.modules.loading[modulePath] = true
	defer func() {
		delete(evaluator.modules.loading, modulePath)
	}()
	moduleThread := &starlark.Thread{Name: modulePath, Load: evaluator.loadModule}
	globals, operationError := starlark.ExecFileOptions(&syntax.FileOptions{}, moduleThread, modulePath, source, evaluator.predeclared())
	if operationError != nil {
		return nil, fmt.Errorf("failed to load module %s: %w", module, operationError)
	}

	// Cache the result before returning
	evaluator.modules.cache[modulePath] = globals

	return globals, nil
}

// targetBuiltin implements the target() function in Starlark.
func (c *starlarkPackageCollector) targetBuiltin(thread *starlark.Thread, fn *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var name string
	var command string
	var dependencies *starlark.List
	var inputs *starlark.List
	var excludeInputs *starlark.List
	var outputs *starlark.List
	var binOutput string
	var binaryRequiresPush bool
	var outputChecks *starlark.List
	var tags *starlark.List
	var fingerprint *starlark.Dict
	var platforms *starlark.List
	var envVars *starlark.Dict
	var timeout string
	var concurrencyGroup string
	var ociPush *starlark.Dict

	if operationError := unpackStarlarkArgs(targetDeclarationKind, args, kwargs,
		&name,
		&command,
		&dependencies,
		&inputs,
		&excludeInputs,
		&outputs,
		&binOutput,
		&binaryRequiresPush,
		&outputChecks,
		&tags,
		&fingerprint,
		&platforms,
		&envVars,
		&timeout,
		&concurrencyGroup,
		&ociPush,
	); operationError != nil {
		return nil, operationError
	}

	target := &TargetDTO{
		Name:    name,
		Command: command,
	}

	// Convert dependencies
	if dependencies != nil {
		deps, err := starlarkListToStringSlice(dependencies)
		if err != nil {
			return nil, fmt.Errorf("dependencies: %w", err)
		}
		target.Dependencies = deps
	}

	// Convert inputs
	if inputs != nil {
		inp, err := starlarkListToStringSlice(inputs)
		if err != nil {
			return nil, fmt.Errorf("inputs: %w", err)
		}
		target.Inputs = inp
	}

	// Convert exclude_inputs
	if excludeInputs != nil {
		excl, err := starlarkListToStringSlice(excludeInputs)
		if err != nil {
			return nil, fmt.Errorf("exclude_inputs: %w", err)
		}
		target.ExcludeInputs = excl
	}

	// Convert outputs
	if outputs != nil {
		out, err := starlarkListToStringSlice(outputs)
		if err != nil {
			return nil, fmt.Errorf("outputs: %w", err)
		}
		target.Outputs = out
	}

	// Set bin_output
	if binOutput != "" {
		target.BinOutput = binOutput
	}

	target.BinaryRequiresPush = binaryRequiresPush

	// Convert output_checks
	if outputChecks != nil {
		checks, err := starlarkListToOutputChecks(outputChecks)
		if err != nil {
			return nil, fmt.Errorf("output_checks: %w", err)
		}
		target.OutputChecks = checks
	}

	// Convert tags
	if tags != nil {
		t, err := starlarkListToStringSlice(tags)
		if err != nil {
			return nil, fmt.Errorf("tags: %w", err)
		}
		target.Tags = t
	}

	// Convert fingerprint
	if fingerprint != nil {
		fp, err := starlarkDictToStringMap(fingerprint)
		if err != nil {
			return nil, fmt.Errorf("fingerprint: %w", err)
		}
		target.Fingerprint = fp
	}

	// Convert platforms
	if platforms != nil {
		plat, err := starlarkListToStringSlice(platforms)
		if err != nil {
			return nil, fmt.Errorf("platforms: %w", err)
		}
		target.Platforms = plat
	}

	// Convert environment_variables
	if envVars != nil {
		ev, err := starlarkDictToStringMap(envVars)
		if err != nil {
			return nil, fmt.Errorf("environment_variables: %w", err)
		}
		target.EnvironmentVariables = ev
	}

	// Set timeout
	if timeout != "" {
		target.Timeout = timeout
	}

	if concurrencyGroup != "" {
		target.ConcurrencyGroup = concurrencyGroup
	}

	if ociPush != nil {
		push, err := starlarkDictToOciPush(ociPush)
		if err != nil {
			return nil, fmt.Errorf("oci_push: %w", err)
		}
		target.OciPush = push
	}

	c.targets = append(c.targets, target)
	c.recordDeclaration(thread, targetDeclarationKind, name)
	return starlark.None, nil
}

// aliasBuiltin implements the alias() function in Starlark.
func (c *starlarkPackageCollector) aliasBuiltin(thread *starlark.Thread, fn *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var name string
	var actual string

	if operationError := unpackStarlarkArgs(aliasDeclarationKind, args, kwargs,
		&name,
		&actual,
	); operationError != nil {
		return nil, operationError
	}

	alias := &AliasDTO{
		Name:   name,
		Actual: actual,
	}

	c.aliases = append(c.aliases, alias)
	c.recordDeclaration(thread, aliasDeclarationKind, name)
	return starlark.None, nil
}

// resourceBuiltin implements the resource() function in Starlark.
func (c *starlarkPackageCollector) resourceBuiltin(thread *starlark.Thread, fn *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var name string
	var up string
	var down string
	var ready string
	var timeout string
	var exports *starlark.Dict
	var dependencies *starlark.List

	if operationError := unpackStarlarkArgs(resourceDeclarationKind, args, kwargs,
		&name,
		&up,
		&down,
		&ready,
		&timeout,
		&exports,
		&dependencies,
	); operationError != nil {
		return nil, operationError
	}

	resource := &ResourceDTO{
		Name:    name,
		Up:      up,
		Down:    down,
		Ready:   ready,
		Timeout: timeout,
	}

	if exports != nil {
		exportMap, err := starlarkDictToStringMap(exports)
		if err != nil {
			return nil, fmt.Errorf("exports: %w", err)
		}
		resource.Exports = exportMap
	}

	if dependencies != nil {
		deps, err := starlarkListToStringSlice(dependencies)
		if err != nil {
			return nil, fmt.Errorf("dependencies: %w", err)
		}
		resource.Dependencies = deps
	}

	c.resources = append(c.resources, resource)
	c.recordDeclaration(thread, resourceDeclarationKind, name)
	return starlark.None, nil
}

// environmentBuiltin implements the environment() function in Starlark.
func (c *starlarkPackageCollector) environmentBuiltin(thread *starlark.Thread, fn *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var name string
	var envType string
	var dependencies *starlark.List
	var ociImage string

	if operationError := unpackStarlarkArgs(environmentDeclarationKind, args, kwargs,
		&name,
		&envType,
		&dependencies,
		&ociImage,
	); operationError != nil {
		return nil, operationError
	}

	env := &EnvironmentDTO{
		Name:     name,
		Type:     envType,
		OCIImage: ociImage,
	}

	// Convert dependencies
	if dependencies != nil {
		deps, err := starlarkListToStringSlice(dependencies)
		if err != nil {
			return nil, fmt.Errorf("dependencies: %w", err)
		}
		env.Dependencies = deps
	}

	c.environments = append(c.environments, env)
	c.recordDeclaration(thread, environmentDeclarationKind, name)
	return starlark.None, nil
}

func (collector *starlarkPackageCollector) recordDeclaration(thread *starlark.Thread, kind string, name string) {
	if collector.declaration == nil {
		return
	}
	position := thread.CallFrame(1).Pos
	collector.declaration(StarlarkDeclaration{Kind: kind, Name: name, Path: position.Filename(), Line: int(position.Line), Column: int(position.Col)})
}

func starlarkListToStringSlice(list *starlark.List) ([]string, error) {
	result := make([]string, 0, list.Len())
	iter := list.Iterate()
	defer iter.Done()
	var val starlark.Value
	for iter.Next(&val) {
		str, ok := val.(starlark.String)
		if !ok {
			return nil, fmt.Errorf("expected string, got %s", val.Type()) //nolint:nilaway // Iterate assigns val before Next reports true.
		}
		result = append(result, string(str))
	}
	return result, nil
}

// starlarkDictToOciPush converts an oci_push dict where each value is either
// a single destination string or a list of strings. The DTO normalises
// scalars to a single-element list so downstream code only deals with []string.
func starlarkDictToOciPush(dict *starlark.Dict) (map[string]ociPushDestinations, error) {
	result := make(map[string]ociPushDestinations, dict.Len())
	for _, item := range dict.Items() {
		key, ok := item[0].(starlark.String)
		if !ok {
			return nil, fmt.Errorf("oci_push key must be string, got %s", item[0].Type())
		}
		switch v := item[1].(type) {
		case starlark.String:
			result[string(key)] = ociPushDestinations{string(v)}
		case *starlark.List:
			dst, err := starlarkListToStringSlice(v)
			if err != nil {
				return nil, fmt.Errorf("oci_push[%q]: %w", string(key), err)
			}
			result[string(key)] = dst
		default:
			return nil, fmt.Errorf("oci_push[%q] must be string or list of strings, got %s", string(key), v.Type())
		}
	}
	return result, nil
}

func starlarkDictToStringMap(dict *starlark.Dict) (map[string]string, error) {
	result := make(map[string]string)
	for _, item := range dict.Items() {
		key, ok := item[0].(starlark.String)
		if !ok {
			return nil, fmt.Errorf("dict key must be string, got %s", item[0].Type())
		}
		val, ok := item[1].(starlark.String)
		if !ok {
			return nil, fmt.Errorf("dict value must be string, got %s", item[1].Type())
		}
		result[string(key)] = string(val)
	}
	return result, nil
}

func starlarkListToOutputChecks(list *starlark.List) ([]model.OutputCheck, error) {
	result := make([]model.OutputCheck, 0, list.Len())
	iter := list.Iterate()
	defer iter.Done()
	var val starlark.Value
	for iter.Next(&val) {
		// Output checks can be dicts or structs
		var command, expectedOutput string

		if dict, ok := val.(*starlark.Dict); ok {
			// Handle dict format
			cmdVal, found, err := dict.Get(starlark.String("command"))
			if err != nil {
				return nil, err
			}
			if !found {
				return nil, fmt.Errorf("output_check missing 'command' field")
			}
			cmdStr, ok := cmdVal.(starlark.String)
			if !ok {
				return nil, fmt.Errorf("output_check 'command' must be string")
			}
			command = string(cmdStr)

			expVal, found, _ := dict.Get(starlark.String("expected_output"))
			if found {
				if expStr, ok := expVal.(starlark.String); ok {
					expectedOutput = string(expStr)
				}
			}
		} else if structVal, ok := val.(*starlarkstruct.Struct); ok {
			// Handle struct format
			cmdVal, err := structVal.Attr("command")
			if err != nil {
				return nil, fmt.Errorf("output_check missing 'command' field")
			}
			cmdStr, ok := cmdVal.(starlark.String)
			if !ok {
				return nil, fmt.Errorf("output_check 'command' must be string")
			}
			command = string(cmdStr)

			expVal, err := structVal.Attr("expected_output")
			if err == nil {
				if expStr, ok := expVal.(starlark.String); ok {
					expectedOutput = string(expStr)
				}
			}
		} else {
			return nil, fmt.Errorf("output_check must be dict or struct, got %s", val.Type()) //nolint:nilaway // Iterate assigns val before Next reports true.
		}

		result = append(result, model.OutputCheck{
			Command:        command,
			ExpectedOutput: expectedOutput,
		})
	}
	return result, nil
}

// resolvedEnvironmentVariablesFilePath returns the absolute path to the
// configured environment variables file. If EnvironmentVariablesFile is empty,
// it returns an empty string. Relative paths are resolved against WorkspaceRoot.
func resolvedEnvironmentVariablesFilePath() string {
	environmentVariablesFilePath := config.Global.EnvironmentVariablesFile
	if environmentVariablesFilePath == "" {
		return ""
	}
	if !filepath.IsAbs(environmentVariablesFilePath) {
		environmentVariablesFilePath = filepath.Join(config.Global.WorkspaceRoot, environmentVariablesFilePath)
	}
	return filepath.Clean(environmentVariablesFilePath)
}
