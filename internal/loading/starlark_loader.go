package loading

import (
	"context"
	"fmt"
	"grog/internal/config"
	"grog/internal/model"
	"os"
	"path/filepath"

	"go.starlark.net/starlark"
	"go.starlark.net/starlarkstruct"
)

// StarlarkLoader implements the Loader interface for Starlark files.
type StarlarkLoader struct{}

func (sl StarlarkLoader) Matches(fileName string) bool {
	return fileName == "BUILD.star" || fileName == "BUILD.bzl"
}

// starlarkPackageCollector holds the collected targets, aliases, and environments
type starlarkPackageCollector struct {
	targets          []*TargetDTO
	aliases          []*AliasDTO
	environments     []*EnvironmentDTO
	defaultPlatforms []string
}

// Load reads the file at the specified filePath and evaluates it as Starlark code.
func (sl StarlarkLoader) Load(ctx context.Context, filePath string) (PackageDTO, bool, error) {
	collector := &starlarkPackageCollector{
		targets:      make([]*TargetDTO, 0),
		aliases:      make([]*AliasDTO, 0),
		environments: make([]*EnvironmentDTO, 0),
	}

	// Create predeclared functions and values
	predeclared := starlark.StringDict{
		"target":      starlark.NewBuiltin("target", collector.targetBuiltin),
		"alias":       starlark.NewBuiltin("alias", collector.aliasBuiltin),
		"environment": starlark.NewBuiltin("environment", collector.environmentBuiltin),
		"GROG_OS":     starlark.String(config.Global.OS),
		"GROG_ARCH":   starlark.String(config.Global.Arch),
		"GROG_PLATFORM": starlark.String(config.Global.GetPlatform()),
	}

	// Add environment variables to predeclared
	for k, v := range config.Global.EnvironmentVariables {
		predeclared[k] = starlark.String(v)
	}

	thread := &starlark.Thread{
		Name: filePath,
		Load: func(thread *starlark.Thread, module string) (starlark.StringDict, error) {
			return sl.loadModule(thread, module, filePath, collector)
		},
	}

	// Execute the Starlark file
	_, err := starlark.ExecFile(thread, filePath, nil, predeclared)
	if err != nil {
		return PackageDTO{}, false, fmt.Errorf("failed to evaluate Starlark file %s: %w", filePath, err)
	}

	pkg := PackageDTO{
		Targets:          collector.targets,
		Aliases:          collector.aliases,
		Environments:     collector.environments,
		DefaultPlatforms: collector.defaultPlatforms,
	}

	return pkg, true, nil
}

// loadModule implements the load() function for importing other Starlark files
func (sl StarlarkLoader) loadModule(thread *starlark.Thread, module string, currentFile string, collector *starlarkPackageCollector) (starlark.StringDict, error) {
	// Resolve module path relative to workspace root or current file
	var modulePath string

	if len(module) > 2 && module[:2] == "//" {
		// Absolute path from workspace root
		relPath := module[2:]
		modulePath = filepath.Join(config.Global.WorkspaceRoot, relPath)
	} else {
		// Relative path from current file
		currentDir := filepath.Dir(currentFile)
		modulePath = filepath.Join(currentDir, module)
	}

	// Check if file exists
	if _, err := os.Stat(modulePath); os.IsNotExist(err) {
		return nil, fmt.Errorf("module not found: %s (resolved to %s)", module, modulePath)
	}

	// Create predeclared functions for the loaded module
	predeclared := starlark.StringDict{
		"target":      starlark.NewBuiltin("target", collector.targetBuiltin),
		"alias":       starlark.NewBuiltin("alias", collector.aliasBuiltin),
		"environment": starlark.NewBuiltin("environment", collector.environmentBuiltin),
		"GROG_OS":     starlark.String(config.Global.OS),
		"GROG_ARCH":   starlark.String(config.Global.Arch),
		"GROG_PLATFORM": starlark.String(config.Global.GetPlatform()),
	}

	// Add environment variables
	for k, v := range config.Global.EnvironmentVariables {
		predeclared[k] = starlark.String(v)
	}

	// Create a new thread for the module with the same load function
	moduleThread := &starlark.Thread{
		Name: modulePath,
		Load: func(t *starlark.Thread, m string) (starlark.StringDict, error) {
			return sl.loadModule(t, m, modulePath, collector)
		},
	}

	// Execute the module
	globals, err := starlark.ExecFile(moduleThread, modulePath, nil, predeclared)
	if err != nil {
		return nil, fmt.Errorf("failed to load module %s: %w", module, err)
	}

	return globals, nil
}

// targetBuiltin implements the target() function in Starlark
func (c *starlarkPackageCollector) targetBuiltin(thread *starlark.Thread, fn *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var name string
	var command string
	var dependencies *starlark.List
	var inputs *starlark.List
	var excludeInputs *starlark.List
	var outputs *starlark.List
	var binOutput string
	var outputChecks *starlark.List
	var tags *starlark.List
	var fingerprint *starlark.Dict
	var platforms *starlark.List
	var envVars *starlark.Dict
	var timeout string

	// Parse keyword arguments
	if err := starlark.UnpackArgs("target", args, kwargs,
		"name", &name,
		"command?", &command,
		"dependencies?", &dependencies,
		"inputs?", &inputs,
		"exclude_inputs?", &excludeInputs,
		"outputs?", &outputs,
		"bin_output?", &binOutput,
		"output_checks?", &outputChecks,
		"tags?", &tags,
		"fingerprint?", &fingerprint,
		"platforms?", &platforms,
		"environment_variables?", &envVars,
		"timeout?", &timeout,
	); err != nil {
		return nil, err
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

	c.targets = append(c.targets, target)
	return starlark.None, nil
}

// aliasBuiltin implements the alias() function in Starlark
func (c *starlarkPackageCollector) aliasBuiltin(thread *starlark.Thread, fn *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var name string
	var actual string

	if err := starlark.UnpackArgs("alias", args, kwargs,
		"name", &name,
		"actual", &actual,
	); err != nil {
		return nil, err
	}

	alias := &AliasDTO{
		Name:   name,
		Actual: actual,
	}

	c.aliases = append(c.aliases, alias)
	return starlark.None, nil
}

// environmentBuiltin implements the environment() function in Starlark
func (c *starlarkPackageCollector) environmentBuiltin(thread *starlark.Thread, fn *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var name string
	var envType string
	var dependencies *starlark.List
	var dockerImage string

	if err := starlark.UnpackArgs("environment", args, kwargs,
		"name", &name,
		"type", &envType,
		"dependencies?", &dependencies,
		"docker_image?", &dockerImage,
	); err != nil {
		return nil, err
	}

	env := &EnvironmentDTO{
		Name:        name,
		Type:        envType,
		DockerImage: dockerImage,
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
	return starlark.None, nil
}

// Helper functions to convert Starlark types to Go types

func starlarkListToStringSlice(list *starlark.List) ([]string, error) {
	result := make([]string, 0, list.Len())
	iter := list.Iterate()
	defer iter.Done()
	var val starlark.Value
	for iter.Next(&val) {
		str, ok := val.(starlark.String)
		if !ok {
			return nil, fmt.Errorf("expected string, got %s", val.Type())
		}
		result = append(result, string(str))
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
			return nil, fmt.Errorf("output_check must be dict or struct, got %s", val.Type())
		}

		result = append(result, model.OutputCheck{
			Command:        command,
			ExpectedOutput: expectedOutput,
		})
	}
	return result, nil
}
