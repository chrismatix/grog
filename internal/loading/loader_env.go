package loading

import (
	"strings"

	"grog/internal/config"

	"go.starlark.net/starlark"
)

// LoaderEnv returns the GROG_* values exposed to both the pkl and starlark
// loaders. Every entry must also be available to the build command via
// execution.GetExtendedTargetEnv — anything BUILD files can read at load
// time should also be readable at execute time and vice versa.
//
// Per-target values (GROG_TARGET, GROG_PACKAGE) are intentionally excluded;
// the loader does not know which target a value would belong to.
func LoaderEnv() map[string]string {
	return map[string]string{
		"GROG_OS":             config.Global.OS,
		"GROG_ARCH":           config.Global.Arch,
		"GROG_PLATFORM":       config.Global.GetPlatform(),
		"GROG_PLATFORM_TAGS":  strings.Join(config.Global.PlatformTags, ","),
		"GROG_ENV_FILE":       resolvedEnvironmentVariablesFilePath(),
		"GROG_WORKSPACE_ROOT": config.Global.WorkspaceRoot,
		"GROG_GIT_HASH":       loaderGitHash(),
	}
}

// loaderGitHash mirrors execution.GetExtendedTargetEnv: outside a git repo,
// the variable resolves to the empty string instead of failing the load.
func loaderGitHash() string {
	hash, _ := config.GetGitHash()
	return hash
}

// addLoaderEnvToStarlark mirrors LoaderEnv into a starlark predeclared dict.
// GROG_PLATFORM_TAGS becomes a starlark list rather than the comma-joined
// string used by pkl, matching the existing per-language convention.
func addLoaderEnvToStarlark(dict starlark.StringDict) {
	dict["GROG_OS"] = starlark.String(config.Global.OS)
	dict["GROG_ARCH"] = starlark.String(config.Global.Arch)
	dict["GROG_PLATFORM"] = starlark.String(config.Global.GetPlatform())
	dict["GROG_PLATFORM_TAGS"] = platformTagsStarlarkList()
	dict["GROG_ENV_FILE"] = starlark.String(resolvedEnvironmentVariablesFilePath())
	dict["GROG_WORKSPACE_ROOT"] = starlark.String(config.Global.WorkspaceRoot)
	dict["GROG_GIT_HASH"] = starlark.String(loaderGitHash())
}
