package shell

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

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
	missingDirectory := filepath.Join(t.TempDir(), "missing")
	for _, name := range []string{"TMPDIR", "TMP", "TEMP"} {
		t.Setenv(name, missingDirectory)
	}

	if _, _, err := NewCommand(context.Background(), "true"); err == nil {
		t.Fatal("expected an error when the temp directory does not exist")
	}
}

func TestCommandArguments(t *testing.T) {
	scriptPath := filepath.Join(t.TempDir(), "script with spaces.sh")
	if err := os.WriteFile(scriptPath, []byte(`printf '%s\n' "$@"`), 0600); err != nil {
		t.Fatal(err)
	}
	output, err := Command(t.Context(), scriptPath, "two words", "a&b", "").CombinedOutput()
	if err != nil || string(output) != "two words\na&b\n\n" {
		t.Fatalf("got %q, %v", output, err)
	}
}

func TestCommandCancellation(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 200*time.Millisecond)
	defer cancel()
	started := time.Now()
	output, err := Command(ctx, "-c", "sleep 30 & wait").CombinedOutput()
	if err == nil || !errors.Is(ctx.Err(), context.DeadlineExceeded) || time.Since(started) > 5*time.Second {
		t.Fatalf("cancellation took %s: %v, output %s", time.Since(started), err, strings.TrimSpace(string(output)))
	}
}
