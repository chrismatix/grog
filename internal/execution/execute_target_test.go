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

	command, err := getCommand(nil, nil, nil, nil, "echo hi")
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

	command, err := getCommand(nil, nil, nil, nil, "echo hi")
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if strings.Contains(command, "set -eu") {
		t.Fatalf("expected command not to include default shell flags, got %q", command)
	}
}

// ---------------------------------------------------------------------------
// transitive_outputs shell function tests
// ---------------------------------------------------------------------------

// TestTransitiveOutputsShellFunctionReturnsAllOutputs verifies that the
// transitive_outputs() shell function echoes all transitive output paths.
func TestTransitiveOutputsShellFunctionReturnsAllOutputs(t *testing.T) {
	original := config.Global.DisableDefaultShellFlags
	config.Global.DisableDefaultShellFlags = true
	t.Cleanup(func() {
		config.Global.DisableDefaultShellFlags = original
	})

	transitiveOutputs := []string{"/workspace/pkg/dist/a", "/workspace/lib/out.so"}

	command, err := getCommand(nil, nil, transitiveOutputs, nil, `echo "$(transitive_outputs)"`)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if !strings.Contains(command, "/workspace/pkg/dist/a") {
		t.Errorf("expected command to contain first output path, got:\n%s", command)
	}
	if !strings.Contains(command, "/workspace/lib/out.so") {
		t.Errorf("expected command to contain second output path, got:\n%s", command)
	}
	if !strings.Contains(command, "transitive_outputs()") {
		t.Errorf("expected command to define transitive_outputs function, got:\n%s", command)
	}
}

// TestTransitiveOutputsShellFunctionEmptyWhenNil verifies that
// transitive_outputs() produces no output when there are no transitive deps.
func TestTransitiveOutputsShellFunctionEmptyWhenNil(t *testing.T) {
	original := config.Global.DisableDefaultShellFlags
	config.Global.DisableDefaultShellFlags = true
	t.Cleanup(func() {
		config.Global.DisableDefaultShellFlags = original
	})

	command, err := getCommand(nil, nil, nil, nil, "echo hello")
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	// Function should still be defined
	if !strings.Contains(command, "transitive_outputs()") {
		t.Errorf("expected transitive_outputs function to be defined, got:\n%s", command)
	}
}

// ---------------------------------------------------------------------------
// transitive_outputs_by_tag shell function tests
// ---------------------------------------------------------------------------

// TestTransitiveOutputsByTagShellFunctionReturnsNewlineSeparatedPaths verifies
// that the transitive_outputs_by_tag() shell function template expands to a
// case statement that echoes newline-separated output paths for the requested tag.
func TestTransitiveOutputsByTagShellFunctionReturnsNewlineSeparatedPaths(t *testing.T) {
	original := config.Global.DisableDefaultShellFlags
	config.Global.DisableDefaultShellFlags = true
	t.Cleanup(func() {
		config.Global.DisableDefaultShellFlags = original
	})

	taggedOutputs := TransitiveTaggedOutputs{
		"find-links": {"/workspace/pkg/dist/a", "/workspace/pkg/dist/c"},
	}

	command, err := getCommand(nil, nil, nil, taggedOutputs, `echo "$(transitive_outputs_by_tag find-links)"`)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if !strings.Contains(command, "/workspace/pkg/dist/a") {
		t.Errorf("expected command to contain first output path, got:\n%s", command)
	}
	if !strings.Contains(command, "/workspace/pkg/dist/c") {
		t.Errorf("expected command to contain second output path, got:\n%s", command)
	}
	if !strings.Contains(command, "transitive_outputs_by_tag") {
		t.Errorf("expected command to define transitive_outputs_by_tag function, got:\n%s", command)
	}
}

// TestTransitiveOutputsByTagShellFunctionErrorsOnUnknownTag verifies the
// generated shell function produces an error message for an unrecognised tag.
func TestTransitiveOutputsByTagShellFunctionErrorsOnUnknownTag(t *testing.T) {
	original := config.Global.DisableDefaultShellFlags
	config.Global.DisableDefaultShellFlags = true
	t.Cleanup(func() {
		config.Global.DisableDefaultShellFlags = original
	})

	taggedOutputs := TransitiveTaggedOutputs{
		"find-links": {"/workspace/dist"},
	}

	command, err := getCommand(nil, nil, nil, taggedOutputs, `$(transitive_outputs_by_tag no-such-tag)`)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if !strings.Contains(command, "Error: unknown tag") {
		t.Errorf("expected error handler for unknown tag, got:\n%s", command)
	}
}

// TestTransitiveOutputsByTagShellFunctionRendersWhenNoTaggedOutputs verifies
// that when there are no transitive tagged outputs, the function still renders
// with only the error fallback.
func TestTransitiveOutputsByTagShellFunctionRendersWhenNoTaggedOutputs(t *testing.T) {
	original := config.Global.DisableDefaultShellFlags
	config.Global.DisableDefaultShellFlags = true
	t.Cleanup(func() {
		config.Global.DisableDefaultShellFlags = original
	})

	command, err := getCommand(nil, nil, nil, nil, "echo hello")
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if !strings.Contains(command, "transitive_outputs_by_tag") {
		t.Errorf("expected transitive_outputs_by_tag function to be defined even with nil map, got:\n%s", command)
	}
}
