package execution

import (
	"strings"
	"testing"

	"grog/internal/config"
)

func TestGetCommandAddsDefaultShellFlags(t *testing.T) {
	original := config.Global.DisableDefaultShellFlags
	config.Global.DisableDefaultShellFlags = false
	t.Cleanup(func() {
		config.Global.DisableDefaultShellFlags = original
	})

	command, err := getCommand(nil, nil, "echo hi")
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if !strings.Contains(command, "set -eu\necho hi") {
		t.Fatalf("expected command to include default shell flags, got %q", command)
	}
}

func TestGetCommandAllowsDisablingDefaultShellFlags(t *testing.T) {
	original := config.Global.DisableDefaultShellFlags
	config.Global.DisableDefaultShellFlags = true
	t.Cleanup(func() {
		config.Global.DisableDefaultShellFlags = original
	})

	command, err := getCommand(nil, nil, "echo hi")
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if strings.Contains(command, "set -eu") {
		t.Fatalf("expected command not to include default shell flags, got %q", command)
	}
}
