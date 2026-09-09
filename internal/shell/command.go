package shell

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"time"

	"grog/internal/config"
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
	scriptFile, err := os.CreateTemp("", "grog-cmd-*.sh")
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create command script file: %w", err)
	}
	cleanup := func() { _ = os.Remove(scriptFile.Name()) }
	if _, err := scriptFile.WriteString(script); err != nil {
		_ = scriptFile.Close()
		cleanup()
		return nil, nil, fmt.Errorf("failed to write command script file: %w", err)
	}
	if err := scriptFile.Close(); err != nil {
		cleanup()
		return nil, nil, fmt.Errorf("failed to close command script file: %w", err)
	}
	command := exec.CommandContext(commandContext, "sh", append([]string{scriptFile.Name()}, arguments...)...)
	command.WaitDelay = time.Second
	return command, cleanup, nil
}
