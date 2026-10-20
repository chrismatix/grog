package shell

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/chrismatix/grog/internal/config"
)

func TestWithDefaultFlags(t *testing.T) {
	previousConfig := config.Global
	t.Cleanup(func() { config.Global = previousConfig })

	config.Global.DisableDefaultShellFlags = false
	if got := WithDefaultFlags("echo hi"); got != "set -eu\necho hi" {
		t.Fatalf("expected default flags prefix, got %q", got)
	}

	config.Global.DisableDefaultShellFlags = true
	if got := WithDefaultFlags("echo hi"); got != "echo hi" {
		t.Fatalf("expected unchanged command, got %q", got)
	}
}

func TestNewCommandRunsScriptWithArguments(t *testing.T) {
	command, cleanup, err := NewCommand(context.Background(), `echo "$1-$2"`, "first", "second")
	if err != nil {
		t.Fatalf("NewCommand failed: %v", err)
	}
	scriptPath := command.Args[1]

	output, err := command.Output()
	if err != nil {
		t.Fatalf("command failed: %v", err)
	}
	if strings.TrimSpace(string(output)) != "first-second" {
		t.Fatalf("expected first-second, got %q", output)
	}

	cleanup()
	if _, err := os.Stat(scriptPath); !os.IsNotExist(err) {
		t.Fatalf("expected script file to be removed, got %v", err)
	}
}

func TestNewCommandFailsWithoutTempDirectory(t *testing.T) {
	t.Setenv("TMPDIR", filepath.Join(t.TempDir(), "missing"))

	if _, _, err := NewCommand(context.Background(), "true"); err == nil {
		t.Fatal("expected an error when the temp directory does not exist")
	}
}
