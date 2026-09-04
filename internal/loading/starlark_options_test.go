package loading

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"grog/internal/config"
)

func TestStarlarkOptionsForWorkspaceUsesEffectiveConfiguration(t *testing.T) {
	workspaceRoot := t.TempDir()
	previousConfiguration := config.Global
	config.Global = config.WorkspaceConfig{
		WorkspaceRoot:            workspaceRoot,
		OS:                       "custom-os",
		Arch:                     "custom-arch",
		PlatformTags:             []string{"accelerated"},
		EnvironmentVariables:     map[string]string{"PROFILE_VALUE": "enabled"},
		EnvironmentVariablesFile: "profile.env",
	}
	t.Cleanup(func() { config.Global = previousConfiguration })

	options := StarlarkOptionsForWorkspace(workspaceRoot)
	if options.Environment["GROG_PLATFORM"] != "custom-os/custom-arch" || options.Environment["PROFILE_VALUE"] != "enabled" {
		t.Fatalf("unexpected environment: %#v", options.Environment)
	}
	if options.Environment["GROG_ENV_FILE"] != filepath.Join(workspaceRoot, "profile.env") {
		t.Fatalf("GROG_ENV_FILE = %q", options.Environment["GROG_ENV_FILE"])
	}
	if len(options.PlatformTags) != 1 || options.PlatformTags[0] != "accelerated" {
		t.Fatalf("platform tags = %v", options.PlatformTags)
	}
}

func TestStarlarkOptionsForWorkspaceUsesGitHash(t *testing.T) {
	workspaceRoot := t.TempDir()
	if operationError := os.WriteFile(filepath.Join(workspaceRoot, "grog.toml"), []byte("platform_tag = [\"configured\"]\n"), 0o644); operationError != nil {
		t.Fatal(operationError)
	}
	if operationError := os.WriteFile(filepath.Join(workspaceRoot, "file"), []byte("content"), 0o644); operationError != nil {
		t.Fatal(operationError)
	}
	commands := [][]string{
		{"git", "init", "--quiet", workspaceRoot},
		{"git", "-C", workspaceRoot, "add", "file"},
		{"git", "-C", workspaceRoot, "-c", "user.name=Test", "-c", "user.email=test@example.com", "commit", "--quiet", "-m", "initial"},
	}
	for _, arguments := range commands {
		if output, operationError := exec.Command(arguments[0], arguments[1:]...).CombinedOutput(); operationError != nil {
			t.Fatalf("%v: %v: %s", arguments, operationError, output)
		}
	}
	options := StarlarkOptionsForWorkspace(workspaceRoot)
	if options.Environment["GROG_GIT_HASH"] == "" {
		t.Fatal("expected GROG_GIT_HASH")
	}
}
