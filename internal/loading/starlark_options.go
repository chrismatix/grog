package loading

import (
	"maps"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"

	"grog/internal/config"

	"github.com/pelletier/go-toml/v2"
	"github.com/subosito/gotenv"
	"go.starlark.net/starlark"
)

// StarlarkOptionsForWorkspace returns tooling evaluation defaults for a workspace.
func StarlarkOptionsForWorkspace(workspaceRoot string) StarlarkEvaluationOptions {
	if filepath.Clean(config.Global.WorkspaceRoot) == filepath.Clean(workspaceRoot) && config.Global.OS != "" && config.Global.Arch != "" {
		environmentFilePath := config.Global.EnvironmentVariablesFile
		if environmentFilePath != "" && !filepath.IsAbs(environmentFilePath) {
			environmentFilePath = filepath.Join(workspaceRoot, environmentFilePath)
		}
		environment := loaderEnvironment(workspaceRoot, config.Global.OS, config.Global.Arch, strings.Join(config.Global.PlatformTags, ","), environmentFilePath, loaderGitHash(workspaceRoot))
		maps.Copy(environment, config.Global.EnvironmentVariables)
		return StarlarkEvaluationOptions{WorkspaceRoot: workspaceRoot, Environment: environment, PlatformTags: config.Global.PlatformTags}
	}
	operatingSystem := runtime.GOOS
	architecture := runtime.GOARCH
	gitHash := loaderGitHash(workspaceRoot)
	environment := loaderEnvironment(workspaceRoot, operatingSystem, architecture, "", "", gitHash)
	options := StarlarkEvaluationOptions{WorkspaceRoot: workspaceRoot, Environment: environment}
	configurationBytes, operationError := os.ReadFile(filepath.Join(workspaceRoot, "grog.toml"))
	if operationError != nil {
		return options
	}
	var configuration struct {
		EnvironmentVariables     map[string]string `toml:"environment_variables"`
		EnvironmentVariablesFile string            `toml:"environment_variables_file"`
		OperatingSystem          string            `toml:"os"`
		Architecture             string            `toml:"arch"`
		PlatformTags             []string          `toml:"platform_tag"`
	}
	if toml.Unmarshal(configurationBytes, &configuration) != nil {
		return options
	}
	if configuration.OperatingSystem != "" {
		operatingSystem = configuration.OperatingSystem
	}
	if configuration.Architecture != "" {
		architecture = configuration.Architecture
	}
	options.Environment = loaderEnvironment(workspaceRoot, operatingSystem, architecture, strings.Join(configuration.PlatformTags, ","), "", gitHash)
	options.PlatformTags = configuration.PlatformTags
	if configuration.EnvironmentVariablesFile != "" {
		environmentFilePath := configuration.EnvironmentVariablesFile
		if !filepath.IsAbs(environmentFilePath) {
			environmentFilePath = filepath.Join(workspaceRoot, environmentFilePath)
		}
		options.Environment["GROG_ENV_FILE"] = environmentFilePath
		if environmentVariables, readError := gotenv.Read(environmentFilePath); readError == nil {
			for name, value := range environmentVariables {
				options.Environment[name] = value
			}
		}
	}
	for name, value := range configuration.EnvironmentVariables {
		options.Environment[name] = value
	}
	return options
}

func standardStarlarkEnvironment(workspaceRoot string, operatingSystem string, architecture string) map[string]string {
	return loaderEnvironment(workspaceRoot, operatingSystem, architecture, "", "", "")
}

// StarlarkGlobalNames returns the standard modules and environment names.
func StarlarkGlobalNames() []string {
	return starlarkNames(false)
}

// StarlarkBuiltinNames returns the declaration builtins accepted by the loader.
func StarlarkBuiltinNames() []string {
	return starlarkNames(true)
}

func starlarkNames(wantBuiltin bool) []string {
	options := StarlarkEvaluationOptions{Environment: standardStarlarkEnvironment("", "", "")}
	evaluator := starlarkEvaluator{collector: &starlarkPackageCollector{}, options: options}
	predeclared := evaluator.predeclared()
	names := make([]string, 0, len(predeclared))
	for name, value := range predeclared {
		_, isBuiltin := value.(*starlark.Builtin)
		if isBuiltin != wantBuiltin {
			continue
		}
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}
