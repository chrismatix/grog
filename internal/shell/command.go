package shell

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"time"

	"github.com/chrismatix/grog/internal/config"
)

// WithDefaultFlags applies the configured shell failure behavior.
func WithDefaultFlags(command string) string {
	if config.Global.DisableDefaultShellFlags {
		return command
	}
	return "set -eu\n" + command
}

// NewCommand runs a temporary script, avoiding the shell argument size limit.
// The caller must call cleanup after the command finishes.
func NewCommand(commandContext context.Context, script string, arguments ...string) (*exec.Cmd, func(), error) {
	scriptFile, operationError := os.CreateTemp("", "grog-cmd-*.sh")
	if operationError != nil {
		return nil, nil, fmt.Errorf("failed to create command script file: %w", operationError)
	}
	cleanup := func() { _ = os.Remove(scriptFile.Name()) }
	if _, operationError := scriptFile.WriteString(script); operationError != nil {
		_ = scriptFile.Close()
		cleanup()
		return nil, nil, fmt.Errorf("failed to write command script file: %w", operationError)
	}
	if operationError := scriptFile.Close(); operationError != nil {
		cleanup()
		return nil, nil, fmt.Errorf("failed to close command script file: %w", operationError)
	}
	return Command(commandContext, append([]string{scriptFile.Name()}, arguments...)...), cleanup, nil
}

// Command runs a POSIX shell, using Git for Windows when sh is not on PATH.
func Command(commandContext context.Context, arguments ...string) *exec.Cmd {
	shellPath, lookupError := exec.LookPath("sh")
	if lookupError != nil && runtime.GOOS == "windows" {
		if gitPath, gitError := exec.LookPath("git"); gitError == nil {
			shellPath, lookupError = exec.LookPath(filepath.Join(filepath.Dir(gitPath), "..", "bin", "sh.exe"))
		}
	}
	if lookupError != nil {
		command := exec.CommandContext(commandContext, "sh", arguments...)
		command.Err = fmt.Errorf("POSIX shell required; install Git for Windows and add Git to PATH: %w", lookupError)
		return command
	}
	command := exec.CommandContext(commandContext, shellPath, arguments...)
	command.WaitDelay = time.Second
	if runtime.GOOS == "windows" {
		command.Cancel = func() error {
			return exec.Command("taskkill", "/F", "/T", "/PID", strconv.Itoa(command.Process.Pid)).Run()
		}
	}
	return command
}
