package execution

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"grog/internal/config"
	"grog/internal/console"
	"grog/internal/label"
	"grog/internal/model"
)

// runWithTeaCtx builds a context carrying a tea Program (created via StartTaskUI)
// so runTargetCommand exercises the program-aware branches.
func newTeaProgramCtx(t *testing.T) (context.Context, *tea.Program) {
	t.Helper()
	ctx, program, _ := console.StartTaskUI(context.Background())
	t.Cleanup(func() { program.Quit() })
	return ctx, program
}

func TestRunTargetCommandWithTeaProgramAndToggle(t *testing.T) {
	prev := config.Global
	tmp := t.TempDir()
	config.Global = config.WorkspaceConfig{
		Root: tmp, WorkspaceRoot: tmp, DisableDefaultShellFlags: true,
	}
	t.Cleanup(func() { config.Global = prev })
	os.MkdirAll(filepath.Join(tmp, "pkg"), 0o755)
	os.MkdirAll(filepath.Join(tmp, "logs", "pkg"), 0o755)

	ctx, _ := newTeaProgramCtx(t)
	toggle := console.NewStreamLogsToggle(true)
	ctx = console.WithStreamLogsToggle(ctx, toggle)

	tgt := &model.Target{
		Label:   label.TargetLabel{Package: "pkg", Name: "t"},
		Command: `echo with-toggle`,
	}
	out, err := runTargetCommand(ctx, tgt, nil, nil, nil, nil, tgt.Command, false)
	if err != nil {
		t.Fatalf("err=%v out=%s", err, string(out))
	}
	if !strings.Contains(string(out), "with-toggle") {
		t.Fatalf("expected output to contain with-toggle, got %q", string(out))
	}
}

func TestRunTargetCommandWithTeaProgramNoToggle(t *testing.T) {
	prev := config.Global
	tmp := t.TempDir()
	config.Global = config.WorkspaceConfig{
		Root: tmp, WorkspaceRoot: tmp, DisableDefaultShellFlags: true,
	}
	t.Cleanup(func() { config.Global = prev })
	os.MkdirAll(filepath.Join(tmp, "pkg"), 0o755)
	os.MkdirAll(filepath.Join(tmp, "logs", "pkg"), 0o755)

	ctx, _ := newTeaProgramCtx(t)
	tgt := &model.Target{
		Label:   label.TargetLabel{Package: "pkg", Name: "t"},
		Command: `echo no-toggle`,
	}
	out, err := runTargetCommand(ctx, tgt, nil, nil, nil, nil, tgt.Command, false)
	if err != nil {
		t.Fatalf("err=%v out=%s", err, string(out))
	}
	if !strings.Contains(string(out), "no-toggle") {
		t.Fatalf("expected output to contain no-toggle, got %q", string(out))
	}
}

func TestRunTargetCommandWithTeaProgramAndStreamLogsTrue(t *testing.T) {
	prev := config.Global
	tmp := t.TempDir()
	config.Global = config.WorkspaceConfig{
		Root: tmp, WorkspaceRoot: tmp, DisableDefaultShellFlags: true,
	}
	t.Cleanup(func() { config.Global = prev })
	os.MkdirAll(filepath.Join(tmp, "pkg"), 0o755)
	os.MkdirAll(filepath.Join(tmp, "logs", "pkg"), 0o755)

	ctx, _ := newTeaProgramCtx(t)
	tgt := &model.Target{
		Label:   label.TargetLabel{Package: "pkg", Name: "t"},
		Command: `echo streamed`,
	}
	out, err := runTargetCommand(ctx, tgt, nil, nil, nil, nil, tgt.Command, true)
	if err != nil {
		t.Fatalf("err=%v out=%s", err, string(out))
	}
	if !strings.Contains(string(out), "streamed") {
		t.Fatalf("expected output to contain streamed, got %q", string(out))
	}
}
