package loading

import (
	"strings"

	"grog/internal/config"
)

// LoaderEnv returns the GROG_* values exposed to both the pkl and starlark
// loaders. Every entry must also be available to the build command via
// execution.GetExtendedTargetEnv — anything BUILD files can read at load
// time should also be readable at execute time and vice versa.
//
// Per-target values (GROG_TARGET, GROG_PACKAGE) are intentionally excluded;
// the loader does not know which target a value would belong to.
func LoaderEnv() map[string]string {
	return loaderEnvironment(
		config.Global.WorkspaceRoot,
		config.Global.OS,
		config.Global.Arch,
		strings.Join(config.Global.PlatformTags, ","),
		resolvedEnvironmentVariablesFilePath(),
		loaderGitHash(),
	)
}

func loaderEnvironment(workspaceRoot string, operatingSystem string, architecture string, platformTags string, environmentFile string, gitHash string) map[string]string {
	return map[string]string{
		"GROG_OS":             operatingSystem,
		"GROG_ARCH":           architecture,
		"GROG_PLATFORM":       operatingSystem + "/" + architecture,
		"GROG_PLATFORM_TAGS":  platformTags,
		"GROG_ENV_FILE":       environmentFile,
		"GROG_WORKSPACE_ROOT": workspaceRoot,
		"GROG_GIT_HASH":       gitHash,
	}
}

// loaderGitHash mirrors execution.GetExtendedTargetEnv: outside a git repo,
// the variable resolves to the empty string instead of failing the load.
func loaderGitHash() string {
	hash, _ := config.GetGitHash()
	return hash
}
