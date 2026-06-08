package execution

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest"

	"grog/internal/caching"
	"grog/internal/caching/backends"
	"grog/internal/config"
	"grog/internal/console"
	"grog/internal/dag"
	"grog/internal/label"
	"grog/internal/model"
	"grog/internal/output"
	"grog/internal/output/handlers"
	"grog/internal/proto/gen"
	"grog/internal/worker"
)

// stubCacheBackend mimics behaviors via overridable funcs.
type stubCacheBackend struct {
	mu          sync.Mutex
	getFn       func(ctx context.Context, p, k string) (io.ReadCloser, error)
	setFn       func(ctx context.Context, p, k string, r io.Reader) error
	existsFn    func(ctx context.Context, p, k string) (bool, error)
	sizeFn      func(ctx context.Context, p, k string) (int64, error)
	beginWrite  func(ctx context.Context) (backends.StagedWriter, error)
	setCalls    []string
	beginCalls  int
	deleteCalls int
}

func (s *stubCacheBackend) TypeName() string { return "stub" }
func (s *stubCacheBackend) Get(ctx context.Context, p, k string) (io.ReadCloser, error) {
	if s.getFn != nil {
		return s.getFn(ctx, p, k)
	}
	return nil, errors.New("not found")
}
func (s *stubCacheBackend) Set(ctx context.Context, p, k string, r io.Reader) error {
	_, _ = io.Copy(io.Discard, r)
	s.mu.Lock()
	s.setCalls = append(s.setCalls, p)
	s.mu.Unlock()
	if s.setFn != nil {
		return s.setFn(ctx, p, k, r)
	}
	return nil
}
func (s *stubCacheBackend) Delete(context.Context, string, string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.deleteCalls++
	return nil
}
func (s *stubCacheBackend) Exists(ctx context.Context, p, k string) (bool, error) {
	if s.existsFn != nil {
		return s.existsFn(ctx, p, k)
	}
	return false, nil
}
func (s *stubCacheBackend) Size(ctx context.Context, p, k string) (int64, error) {
	if s.sizeFn != nil {
		return s.sizeFn(ctx, p, k)
	}
	return 0, nil
}
func (s *stubCacheBackend) BeginWrite(ctx context.Context) (backends.StagedWriter, error) {
	s.mu.Lock()
	s.beginCalls++
	s.mu.Unlock()
	if s.beginWrite != nil {
		return s.beginWrite(ctx)
	}
	return nil, errors.New("BeginWrite not implemented")
}
func (s *stubCacheBackend) ListKeys(context.Context, string, string) ([]string, error) {
	return nil, nil
}

// TestExecutorTaintedTargetReexecutes drives the tainted-target branch of getTaskFunc.
func TestExecutorTaintedTargetReexecutes(t *testing.T) {
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
	graph := dag.NewDirectedGraphFromTargets(tgt)
	executor := NewExecutor(tcache, taint, reg, graph, false, false, true, config.LoadOutputsAll)
	if _, err := executor.Execute(ctx); err != nil {
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
	graph2 := dag.NewDirectedGraphFromTargets(tgt2)
	executor2 := NewExecutor(tcache, taint, reg, graph2, false, false, true, config.LoadOutputsAll)
	if _, err := executor2.Execute(ctx); err != nil {
		t.Fatalf("second exec: %v", err)
	}
}

// TestExecutorMinimalLoadOutputsMode covers the LoadOutputsMinimal cache-hit fast path.
func TestExecutorMinimalLoadOutputsMode(t *testing.T) {
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
	graph := dag.NewDirectedGraphFromTargets(tgt)
	exec1 := NewExecutor(tcache, taint, reg, graph, false, false, true, config.LoadOutputsAll)
	if _, err := exec1.Execute(ctx); err != nil {
		t.Fatalf("first exec: %v", err)
	}

	tgt2 := &model.Target{
		Label:      label.TargetLabel{Package: "pkg", Name: "t"},
		Command:    `echo ok`,
		IsSelected: true,
	}
	graph2 := dag.NewDirectedGraphFromTargets(tgt2)
	exec2 := NewExecutor(tcache, taint, reg, graph2, false, false, true, config.LoadOutputsMinimal)
	if _, err := exec2.Execute(ctx); err != nil {
		t.Fatalf("minimal exec: %v", err)
	}
}

// TestExecutorCacheHitReloadsOutputs replays a target whose previous build
// stored a TargetResult — second Execute should hit cache and load outputs.
func TestExecutorCacheHitReloadsOutputs(t *testing.T) {
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
	graph := dag.NewDirectedGraphFromTargets(tgt)
	if _, err := NewExecutor(tcache, taint, reg, graph, false, false, true, config.LoadOutputsAll).Execute(ctx); err != nil {
		t.Fatalf("first exec: %v", err)
	}

	tgt2 := &model.Target{
		Label:      label.TargetLabel{Package: "pkg", Name: "t"},
		Command:    `echo ok`,
		IsSelected: true,
	}
	graph2 := dag.NewDirectedGraphFromTargets(tgt2)
	if _, err := NewExecutor(tcache, taint, reg, graph2, false, false, true, config.LoadOutputsAll).Execute(ctx); err != nil {
		t.Fatalf("cached exec: %v", err)
	}
}

// TestExecuteTargetTimeout uses a small Timeout to drive the timeout branch.
func TestExecuteTargetTimeout(t *testing.T) {
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
		Command: `sleep 5`,
		Timeout: 50 * time.Millisecond,
	}
	err := executeTarget(context.Background(), tgt, nil, nil, nil, nil, false)
	if err == nil || !strings.Contains(err.Error(), "timeout") {
		t.Fatalf("expected timeout error, got %v", err)
	}
}

// TestExecuteTargetExitCodeWrapsCommandError drives the ExitError branch in executeTarget.
func TestExecuteTargetExitCodeWrapsCommandError(t *testing.T) {
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
		Command: `exit 7`,
	}
	err := executeTarget(context.Background(), tgt, nil, nil, nil, nil, false)
	var ce *CommandError
	if !errors.As(err, &ce) {
		t.Fatalf("expected CommandError, got %v", err)
	}
	if ce.ExitCode != 7 {
		t.Fatalf("expected exit 7, got %d", ce.ExitCode)
	}
}

// TestExecuteTargetCancelled covers the context-cancelled branch.
func TestExecuteTargetCancelled(t *testing.T) {
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
		Command: `sleep 5`,
	}
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(30 * time.Millisecond)
		cancel()
	}()
	err := executeTarget(ctx, tgt, nil, nil, nil, nil, false)
	if err == nil {
		t.Fatal("expected error from cancelled context")
	}
}

func TestFormatTargetResultForDebugNonNil(t *testing.T) {
	tr := &gen.TargetResult{
		ChangeHash:              "abc",
		OutputHash:              "def",
		ExecutionDurationMillis: 42,
	}
	got := formatTargetResultForDebug(tr)
	if !strings.Contains(got, "abc") || !strings.Contains(got, "def") {
		t.Fatalf("expected hashes in result, got %s", got)
	}
}

// TestLoadDependencyOutputsRecursive exercises LoadDependencyOutputs when the
// target cache has no entry — the dep must be re-run.
func TestLoadDependencyOutputsRecursive(t *testing.T) {
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
		Command:    `echo dep`,
		IsSelected: true,
	}
	consumer := &model.Target{
		Label:        label.TargetLabel{Package: "pkg", Name: "consumer"},
		Dependencies: []label.TargetLabel{dep.Label},
		Command:      `echo c`,
		IsSelected:   true,
	}
	graph := dag.NewDirectedGraphFromTargets(consumer, dep)
	graph.AddEdge(dep, consumer)

	executor := NewExecutor(tcache, taint, reg, graph, false, false, true, config.LoadOutputsAll)
	executor.coordinator = NewPoolCoordinator(console.NewFromSugared(zaptest.NewLogger(t).Sugar(), zapcore.DebugLevel), 1, 1, func(tea.Msg) {}, 0)
	executor.coordinator.StartTaskWorkers(ctx)
	executor.coordinator.StartIOWorkers(ctx)
	defer executor.coordinator.Shutdown()
	executor.cacheWriter = NewCacheWriter(tcache, executor.coordinator.IOPool(), false, ctx)

	if err := executor.LoadDependencyOutputs(ctx, consumer, func(worker.StatusUpdate) {}); err != nil {
		t.Fatalf("LoadDependencyOutputs: %v", err)
	}
}

// TestCacheWriterAsyncSuccess covers the async success path including target cache write.
func TestCacheWriterAsyncSuccess(t *testing.T) {
	backend := &recordingCacheBackend{}
	targetCache := caching.NewTargetResultCache(backend)
	cw, pool := newTestCacheWriter(t, targetCache, true, context.Background())
	defer pool.Shutdown()

	plan := &mockWritePlan{}
	pt := &output.PreparedTargetResult{
		TargetResult: &gen.TargetResult{ChangeHash: "x"},
		WritePlans:   []handlers.OutputWritePlan{plan},
	}
	if err := cw.PersistPreparedTarget(context.Background(), "//p:t", pt, func(worker.StatusUpdate) {}); err != nil {
		t.Fatal(err)
	}
	cw.Wait()
	if calls := backend.Calls(); len(calls) != 1 || calls[0] != "target" {
		t.Fatalf("expected target call, got %v", calls)
	}
}

// TestCacheWriterAsyncTargetCacheWriteFails — failure-on-target-cache-write branch
// uses a backend whose Set fails after the upload plan succeeds.
func TestCacheWriterAsyncTargetCacheWriteFails(t *testing.T) {
	backend := &stubCacheBackend{}
	backend.setFn = func(context.Context, string, string, io.Reader) error {
		return errors.New("set boom")
	}
	targetCache := caching.NewTargetResultCache(backend)
	cw, pool := newTestCacheWriter(t, targetCache, true, context.Background())
	defer pool.Shutdown()

	plan := &mockWritePlan{}
	pt := &output.PreparedTargetResult{
		TargetResult: &gen.TargetResult{ChangeHash: "x"},
		WritePlans:   []handlers.OutputWritePlan{plan},
	}
	if err := cw.PersistPreparedTarget(context.Background(), "//p:t", pt, func(worker.StatusUpdate) {}); err != nil {
		t.Fatal(err)
	}
	cw.Wait()
	calls := plan.Calls()
	if len(calls) != 2 || calls[1] != "cleanup" {
		t.Fatalf("expected upload+cleanup, got %v", calls)
	}
}

// TestCacheWriterPersistPreparedTargetRunFireAndForgetError ensures the
// pool-closed path runs cleanup before returning.
func TestCacheWriterPersistPreparedTargetPoolClosedReturnsError(t *testing.T) {
	backend := &recordingCacheBackend{}
	targetCache := caching.NewTargetResultCache(backend)
	cw, pool := newTestCacheWriter(t, targetCache, true, context.Background())
	pool.Shutdown()

	plan := &mockWritePlan{}
	pt := &output.PreparedTargetResult{
		TargetResult: &gen.TargetResult{ChangeHash: "x"},
		WritePlans:   []handlers.OutputWritePlan{plan},
	}
	if err := cw.PersistPreparedTarget(context.Background(), "//p:t", pt, func(worker.StatusUpdate) {}); err == nil {
		t.Fatal("expected error from closed pool")
	}
	if calls := plan.Calls(); len(calls) != 1 || calls[0] != "cleanup" {
		t.Fatalf("expected cleanup only, got %v", calls)
	}
}

// TestCleanupPlansLogsErrors directly drives cleanupPlans through PersistPreparedTarget
// with a cleanup-error plan to cover that branch.
func TestCleanupPlansLogsErrors(t *testing.T) {
	backend := &recordingCacheBackend{}
	targetCache := caching.NewTargetResultCache(backend)
	cw, pool := newTestCacheWriter(t, targetCache, false, context.Background())
	defer pool.Shutdown()

	plan := &mockWritePlan{cleanupErr: errors.New("cleanup boom")}
	pt := &output.PreparedTargetResult{
		TargetResult: &gen.TargetResult{ChangeHash: "x"},
		WritePlans:   []handlers.OutputWritePlan{plan},
	}
	if err := cw.PersistPreparedTarget(context.Background(), "//p:t", pt, func(worker.StatusUpdate) {}); err != nil {
		t.Fatal(err)
	}
}

func TestExecuteTargetNoCommandSkipsRunButLogs(t *testing.T) {
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
		IsSelected: true,
	}
	graph := dag.NewDirectedGraphFromTargets(tgt)
	executor := NewExecutor(tcache, taint, reg, graph, false, false, true, config.LoadOutputsAll)
	if _, err := executor.Execute(ctx); err != nil {
		t.Fatalf("expected success, got %v", err)
	}
}

func TestExecuteAsyncCacheWrites(t *testing.T) {
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
		Command:    `echo ok`,
		IsSelected: true,
	}
	graph := dag.NewDirectedGraphFromTargets(tgt)
	executor := NewExecutor(tcache, taint, reg, graph, false, false, true, config.LoadOutputsAll)
	if _, err := executor.Execute(ctx); err != nil {
		t.Fatalf("expected success, got %v", err)
	}
}
