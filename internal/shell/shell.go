package shell

import (
	"fmt"
	"os/exec"
	"runtime"
)

// LookupPOSIXShell locates the POSIX sh executable used for target commands.
func LookupPOSIXShell() (string, error) {
	path, err := exec.LookPath("sh")
	if err == nil {
		return path, nil
	}
	if runtime.GOOS == "windows" {
		return "", fmt.Errorf("POSIX sh is required on Windows; install Git Bash or MSYS2 and ensure sh.exe is on PATH: %w", err)
	}
	return "", fmt.Errorf("POSIX sh is required but was not found on PATH: %w", err)
}
