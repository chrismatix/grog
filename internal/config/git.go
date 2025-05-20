package config

import (
	"bytes"
	"os/exec"
	"strings"
)

// GetGitHash returns the current git hash.
func GetGitHash() (string, error) {
	cmd := exec.Command("git", "rev-parse", "HEAD")
	var out bytes.Buffer
	cmd.Stdout = &out

	if err := cmd.Run(); err != nil {
		return "", err
	}
	return strings.TrimSpace(out.String()), nil
}
