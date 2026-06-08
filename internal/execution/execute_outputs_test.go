package execution

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"grog/internal/caching"
	"grog/internal/config"
	"grog/internal/dag"
	"grog/internal/label"
	"grog/internal/model"
	"grog/internal/output/handlers"
)

// TestExecuteTargetWithFileOutputs exercises the OnTargetComplete output-writing
// path that handles targets producing real outputs.
func TestExecuteTargetWithFileOutputs(t *testing.T) {
	prev := config.Global
	tmp := t.TempDir()
	config.Global = config.WorkspaceConfig{
		Root: tmp, WorkspaceRoot: tmp, NumWorkers: 1, NumIOWorkers: 1,
		EnableCache: true, DisableDefaultShellFlags: true,
	}
	t.Cleanup(func() { config.Global = prev })
	os.MkdirAll(filepath.Join(tmp, "pkg"), 0o755)
	os.MkdirAll(filepath.Join(tmp, "logs", "pkg"), 0o755)

	reg, ctx := newRegistry(t)
	tcache := caching.NewTargetResultCache(newDiskBackend(t))
	taint := caching.NewTaintStore()

	tgt := &model.Target{
		Label:      label.TargetLabel{Package: "pkg", Name: "t"},
		Command:    `echo hello > out.txt`,
		IsSelected: true,
		Outputs: []model.Output{
			model.NewOutput(string(handlers.FileHandler), "out.txt"),
		},
	}
	graph := dag.NewDirectedGraphFromTargets(tgt)
	executor := NewExecutor(tcache, taint, reg, graph, false, false, true, config.LoadOutputsAll)
	if _, err := executor.Execute(ctx); err != nil {
		t.Fatalf("expected success, got %v", err)
	}
}

func TestExecuteTargetWithFileOutputsCacheHit(t *testing.T) {
	prev := config.Global
	tmp := t.TempDir()
	config.Global = config.WorkspaceConfig{
		Root: tmp, WorkspaceRoot: tmp, NumWorkers: 1, NumIOWorkers: 1,
		EnableCache: true, DisableDefaultShellFlags: true,
	}
	t.Cleanup(func() { config.Global = prev })
	os.MkdirAll(filepath.Join(tmp, "pkg"), 0o755)
	os.MkdirAll(filepath.Join(tmp, "logs", "pkg"), 0o755)

	reg, ctx := newRegistry(t)
	tcache := caching.NewTargetResultCache(newDiskBackend(t))
	taint := caching.NewTaintStore()

	mkTgt := func() *model.Target {
		return &model.Target{
			Label:      label.TargetLabel{Package: "pkg", Name: "t"},
			Command:    `echo hello > out.txt`,
			IsSelected: true,
			Outputs: []model.Output{
				model.NewOutput(string(handlers.FileHandler), "out.txt"),
			},
		}
	}
	first := mkTgt()
	if _, err := NewExecutor(tcache, taint, reg, dag.NewDirectedGraphFromTargets(first), false, false, true, config.LoadOutputsAll).Execute(ctx); err != nil {
		t.Fatalf("first exec: %v", err)
	}
	second := mkTgt()
	if _, err := NewExecutor(tcache, taint, reg, dag.NewDirectedGraphFromTargets(second), false, false, true, config.LoadOutputsAll).Execute(ctx); err != nil {
		t.Fatalf("cached exec: %v", err)
	}
}

// TestRunTargetCommandWithStreamLogsToggle covers the toggleWriter branch.
func TestRunTargetCommandWithStreamLogsToggle(t *testing.T) {
	prev := config.Global
	tmp := t.TempDir()
	config.Global = config.WorkspaceConfig{
		Root: tmp, WorkspaceRoot: tmp, DisableDefaultShellFlags: true,
	}
	t.Cleanup(func() { config.Global = prev })
	os.MkdirAll(filepath.Join(tmp, "pkg"), 0o755)
	os.MkdirAll(filepath.Join(tmp, "logs", "pkg"), 0o755)

	tgt := &model.Target{
		Label:   label.TargetLabel{Package: "pkg", Name: "t"},
		Command: `echo streamlogs`,
	}

	output, err := runTargetCommand(context.Background(), tgt, nil, nil, nil, nil, tgt.Command, true)
	if err != nil {
		t.Fatalf("got %v out=%s", err, string(output))
	}
}
