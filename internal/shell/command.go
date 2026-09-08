package shell

import (
	"context"
	"fmt"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"time"
)

// Command runs a POSIX shell, using Git for Windows when sh is not on PATH.
func Command(ctx context.Context, arguments ...string) *exec.Cmd {
	shellPath, err := exec.LookPath("sh")
	if err != nil && runtime.GOOS == "windows" {
		if gitPath, gitError := exec.LookPath("git"); gitError == nil {
			shellPath, err = exec.LookPath(filepath.Join(filepath.Dir(gitPath), "..", "bin", "sh.exe"))
		}
	}
	if err != nil {
		command := exec.CommandContext(ctx, "sh", arguments...)
		command.Err = fmt.Errorf("POSIX shell required; install Git for Windows and add Git to PATH: %w", err)
		return command
	}
	command := exec.CommandContext(ctx, shellPath, arguments...)
	command.WaitDelay = time.Second
	if runtime.GOOS == "windows" {
		command.Cancel = func() error {
			return exec.Command("taskkill", "/F", "/T", "/PID", strconv.Itoa(command.Process.Pid)).Run()
		}
	}
	return command
}
