package execution

import (
	"context"
	"os"
	"strings"
	"testing"

	"grog/internal/config"
	"grog/internal/label"
	"grog/internal/model"
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

	// Function should still be defined with a no-op body (POSIX-valid)
	if !strings.Contains(command, "transitive_outputs()") {
		t.Errorf("expected transitive_outputs function to be defined, got:\n%s", command)
	}
	// The empty body must contain ":" (no-op) so sh/dash don't reject it
	// as a syntax error (empty function bodies are invalid in POSIX shell).
	if !strings.Contains(command, ":") {
		t.Errorf("expected empty transitive_outputs function body to contain ':' no-op, got:\n%s", command)
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
// that when there are no transitive tagged outputs, the function renders with
// a no-op body instead of a case statement.
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
	// With no tagged outputs the function body should be ":" not a case statement
	if strings.Contains(command, "Error: unknown tag") {
		t.Errorf("expected no error handler when there are no tagged outputs, got:\n%s", command)
	}
}

// ---------------------------------------------------------------------------
// Extra args context tests
// ---------------------------------------------------------------------------

func TestWithExtraArgsRoundTrip(t *testing.T) {
	ctx := context.Background()
	args := []string{"-k", "test_foo", "-x"}

	ctx = WithExtraArgs(ctx, args)
	got := ExtraArgsFromContext(ctx)

	if len(got) != 3 || got[0] != "-k" || got[1] != "test_foo" || got[2] != "-x" {
		t.Fatalf("expected [-k test_foo -x], got %v", got)
	}
}

func TestExtraArgsFromContextReturnsNilWhenUnset(t *testing.T) {
	ctx := context.Background()
	got := ExtraArgsFromContext(ctx)

	if got != nil {
		t.Fatalf("expected nil, got %v", got)
	}
}

// TestRunTargetCommandForwardsExtraArgs verifies that extra args stored in the
// context are forwarded as shell positional parameters ($@) to the target
// command.
func TestRunTargetCommandForwardsExtraArgs(t *testing.T) {
	tmpDir := t.TempDir()

	prev := config.Global
	config.Global = config.WorkspaceConfig{
		WorkspaceRoot:            tmpDir,
		DisableDefaultShellFlags: true,
	}
	t.Cleanup(func() { config.Global = prev })

	// Create required directories for the target's package and log output
	os.MkdirAll(tmpDir+"/pkg", 0755)
	os.MkdirAll(tmpDir+"/logs/pkg", 0755)

	target := &model.Target{
		Label: label.TargetLabel{Package: "pkg", Name: "test"},
	}

	ctx := context.Background()
	ctx = WithExtraArgs(ctx, []string{"-k", "test_foo", "-x"})

	// The command uses $@ which should expand to the extra args
	output, err := runTargetCommand(ctx, target, nil, nil, nil, nil, `echo "ARGS:$@"`, false)
	if err != nil {
		t.Fatalf("expected no error, got %v\noutput: %s", err, string(output))
	}

	out := string(output)
	if !strings.Contains(out, "ARGS:-k test_foo -x") {
		t.Errorf("expected $@ to expand to extra args, got: %s", out)
	}
}

// TestRunTargetCommandWithoutExtraArgs verifies that $@ expands to empty when
// no extra args are set in the context.
func TestRunTargetCommandWithoutExtraArgs(t *testing.T) {
	tmpDir := t.TempDir()

	prev := config.Global
	config.Global = config.WorkspaceConfig{
		WorkspaceRoot:            tmpDir,
		DisableDefaultShellFlags: true,
	}
	t.Cleanup(func() { config.Global = prev })

	os.MkdirAll(tmpDir+"/pkg", 0755)
	os.MkdirAll(tmpDir+"/logs/pkg", 0755)

	target := &model.Target{
		Label: label.TargetLabel{Package: "pkg", Name: "test"},
	}

	ctx := context.Background()

	output, err := runTargetCommand(ctx, target, nil, nil, nil, nil, `echo "ARGS:$@"`, false)
	if err != nil {
		t.Fatalf("expected no error, got %v\noutput: %s", err, string(output))
	}

	out := string(output)
	if !strings.Contains(out, "ARGS:") {
		t.Errorf("expected ARGS: prefix in output, got: %s", out)
	}
	// $@ should be empty, so output should be exactly "ARGS:"
	trimmed := strings.TrimSpace(out)
	if trimmed != "ARGS:" {
		t.Errorf("expected $@ to be empty (output 'ARGS:'), got: %q", trimmed)
	}
}
