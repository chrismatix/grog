package loading

import (
	"os/exec"
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
	gitHash, _ := config.GetGitHash()
	return loaderEnvironment(
		config.Global.WorkspaceRoot,
		config.Global.OS,
		config.Global.Arch,
		strings.Join(config.Global.PlatformTags, ","),
		resolvedEnvironmentVariablesFilePath(),
		gitHash,
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

func loaderGitHash(workspaceRoot string) string {
	command := exec.Command("git", "-C", workspaceRoot, "rev-parse", "HEAD")
	output, _ := command.Output()
	return strings.TrimSpace(string(output))
}
