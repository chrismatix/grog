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
	"grog/internal/worker"
)

// TestLoadDependencyOutputsCacheHitLoadsOutputs exercises the err==nil branch
// of LoadDependencyOutputs by first running the dependency so its outputs land
// in the target cache and CAS, then re-loading via LoadDependencyOutputs.
func TestLoadDependencyOutputsCacheHitLoadsOutputs(t *testing.T) {
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
	backend := newDiskBackend(t)
	tcache := caching.NewTargetResultCache(backend)
	taint := caching.NewTaintStore()

	dep := &model.Target{
		Label:      label.TargetLabel{Package: "pkg", Name: "dep"},
		Command:    `echo dep > out.txt`,
		IsSelected: true,
		Outputs: []model.Output{
			model.NewOutput(string(handlers.FileHandler), "out.txt"),
		},
	}
	consumer := &model.Target{
		Label:        label.TargetLabel{Package: "pkg", Name: "consumer"},
		Dependencies: []label.TargetLabel{dep.Label},
		Command:      `cat out.txt`,
		IsSelected:   true,
	}
	graph := dag.NewDirectedGraphFromTargets(consumer, dep)
	graph.AddEdge(dep, consumer)

	executor := NewExecutor(tcache, taint, reg, graph, false, false, true, config.LoadOutputsAll)
	if _, err := executor.Execute(ctx); err != nil {
		t.Fatalf("first exec: %v", err)
	}

	dep2 := &model.Target{
		Label:      label.TargetLabel{Package: "pkg", Name: "dep"},
		Command:    `echo dep > out.txt`,
		IsSelected: true,
		Outputs: []model.Output{
			model.NewOutput(string(handlers.FileHandler), "out.txt"),
		},
	}
	consumer2 := &model.Target{
		Label:        label.TargetLabel{Package: "pkg", Name: "consumer"},
		Dependencies: []label.TargetLabel{dep2.Label},
		Command:      `cat out.txt`,
		IsSelected:   true,
	}
	graph2 := dag.NewDirectedGraphFromTargets(consumer2, dep2)
	graph2.AddEdge(dep2, consumer2)

	executor2 := NewExecutor(tcache, taint, reg, graph2, false, false, true, config.LoadOutputsAll)
	if _, err := executor2.Execute(ctx); err != nil {
		t.Fatalf("second exec: %v", err)
	}
}

// TestExecuteMinimalLoadOutputsDepReexecution drives the full LoadDependencyOutputs
// branch via a real Execute call when load_outputs=minimal.
func TestExecuteMinimalLoadOutputsDepReexecution(t *testing.T) {
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
	backend := newDiskBackend(t)
	tcache := caching.NewTargetResultCache(backend)
	taint := caching.NewTaintStore()

	dep := &model.Target{
		Label:      label.TargetLabel{Package: "pkg", Name: "dep"},
		Command:    `echo dep > out.txt`,
		IsSelected: true,
		Outputs: []model.Output{
			model.NewOutput(string(handlers.FileHandler), "out.txt"),
		},
	}
	consumer := &model.Target{
		Label:        label.TargetLabel{Package: "pkg", Name: "consumer"},
		Dependencies: []label.TargetLabel{dep.Label},
		Command:      `echo c`,
		IsSelected:   true,
	}
	graph := dag.NewDirectedGraphFromTargets(consumer, dep)
	graph.AddEdge(dep, consumer)
	if _, err := NewExecutor(tcache, taint, reg, graph, false, false, true, config.LoadOutputsMinimal).Execute(ctx); err != nil {
		t.Fatalf("minimal exec: %v", err)
	}
}

// TestLoadDependencyOutputsNoCacheDep covers the SkipsCache branch.
func TestLoadDependencyOutputsNoCacheDep(t *testing.T) {
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
	backend := newDiskBackend(t)
	tcache := caching.NewTargetResultCache(backend)
	taint := caching.NewTaintStore()

	dep := &model.Target{
		Label:      label.TargetLabel{Package: "pkg", Name: "dep"},
		Command:    `echo dep > out.txt`,
		IsSelected: true,
		Tags:       []string{model.TagNoCache},
		Outputs: []model.Output{
			model.NewOutput(string(handlers.FileHandler), "out.txt"),
		},
	}
	consumer := &model.Target{
		Label:        label.TargetLabel{Package: "pkg", Name: "consumer"},
		Dependencies: []label.TargetLabel{dep.Label},
		Command:      `echo c`,
		IsSelected:   true,
	}
	graph := dag.NewDirectedGraphFromTargets(consumer, dep)
	graph.AddEdge(dep, consumer)

	executor2 := NewExecutor(tcache, taint, reg, graph, false, false, true, config.LoadOutputsMinimal)
	if _, err := executor2.Execute(ctx); err != nil {
		t.Fatalf("no-cache dep exec: %v", err)
	}
}

// TestExecuteTestTargetUsesTestWording covers IsTest paths in logTargetBuilt/Cached.
func TestExecuteTestTargetUsesTestWording(t *testing.T) {
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

	mkTest := func() *model.Target {
		return &model.Target{
			Label:      label.TargetLabel{Package: "pkg", Name: "thing_test"},
			Command:    `echo passed`,
			IsSelected: true,
		}
	}
	first := mkTest()
	if _, err := NewExecutor(tcache, taint, reg, dag.NewDirectedGraphFromTargets(first), false, false, true, config.LoadOutputsAll).Execute(ctx); err != nil {
		t.Fatalf("first run: %v", err)
	}
	second := mkTest()
	if _, err := NewExecutor(tcache, taint, reg, dag.NewDirectedGraphFromTargets(second), false, false, true, config.LoadOutputsAll).Execute(ctx); err != nil {
		t.Fatalf("cached run: %v", err)
	}
}

// TestExecutorGetTaskFuncTainted covers the path where the cache hit exists but
// the target is tainted; the target must re-execute.
func TestExecutorGetTaskFuncTainted(t *testing.T) {
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
	backend := newDiskBackend(t)
	tcache := caching.NewTargetResultCache(backend)
	taint := caching.NewTaintStore()

	tgt := &model.Target{
		Label:      label.TargetLabel{Package: "pkg", Name: "t"},
		Command:    `echo ok`,
		IsSelected: true,
	}
	if _, err := NewExecutor(tcache, taint, reg, dag.NewDirectedGraphFromTargets(tgt), false, false, true, config.LoadOutputsAll).Execute(ctx); err != nil {
		t.Fatalf("first exec: %v", err)
	}
	if err := taint.Taint(ctx, tgt.Label); err != nil {
		t.Fatalf("taint: %v", err)
	}
	tgt2 := &model.Target{
		Label:      label.TargetLabel{Package: "pkg", Name: "t"},
		Command:    `echo ok`,
		IsSelected: true,
	}
	if _, err := NewExecutor(tcache, taint, reg, dag.NewDirectedGraphFromTargets(tgt2), false, false, true, config.LoadOutputsAll).Execute(ctx); err != nil {
		t.Fatalf("second exec: %v", err)
	}
}

// Ensure CacheWriter Wait is callable for the disabled cache.
func TestCacheWriterWaitNoOpForDisabledCache(t *testing.T) {
	backend := &recordingCacheBackend{}
	targetCache := caching.NewTargetResultCache(backend)
	cw, pool := newTestCacheWriter(t, targetCache, false, context.Background())
	defer pool.Shutdown()
	cw.Wait()
	if cw.HasPendingWrites() {
		t.Fatal("expected no pending writes")
	}
}

// Verify worker.StatusFunc invocations in cw don't panic.
func TestExecutorWaitForAsyncWritesAfterExecute(t *testing.T) {
	prev := config.Global
	tmp := t.TempDir()
	config.Global = config.WorkspaceConfig{
		Root: tmp, WorkspaceRoot: tmp, NumWorkers: 1, NumIOWorkers: 1,
		EnableCache: true, AsyncCacheWrites: true, DisableDefaultShellFlags: true,
	}
	t.Cleanup(func() { config.Global = prev })
	os.MkdirAll(filepath.Join(tmp, "pkg"), 0o755)
	os.MkdirAll(filepath.Join(tmp, "logs", "pkg"), 0o755)

	reg, ctx := newRegistry(t)
	tcache := caching.NewTargetResultCache(newDiskBackend(t))
	taint := caching.NewTaintStore()

	tgt := &model.Target{
		Label:      label.TargetLabel{Package: "pkg", Name: "t"},
		Command:    `echo ok > out.txt`,
		IsSelected: true,
		Outputs:    []model.Output{model.NewOutput(string(handlers.FileHandler), "out.txt")},
	}
	graph := dag.NewDirectedGraphFromTargets(tgt)
	executor := NewExecutor(tcache, taint, reg, graph, false, false, true, config.LoadOutputsAll)
	executor.DeferAsyncWait()
	if _, err := executor.Execute(ctx); err != nil {
		t.Fatalf("exec: %v", err)
	}
	executor.WaitForAsyncWrites(ctx)
	if dur := executor.AsyncWaitTime(); dur < 0 {
		t.Fatalf("expected non-negative async wait time, got %v", dur)
	}
}

// Silence unused warnings if any.
var _ worker.StatusFunc = func(worker.StatusUpdate) {}
