package execution

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

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

func newDiskBackend(t *testing.T) backends.CacheBackend {
	t.Helper()
	return backends.NewFileSystemCacheForTest(t.TempDir(), t.TempDir())
}

func newRegistry(t *testing.T) (*output.Registry, context.Context) {
	t.Helper()
	ctx := console.WithLogger(context.Background(),
		console.NewFromSugared(zaptest.NewLogger(t).Sugar(), zapcore.DebugLevel))
	cas := caching.NewCas(newDiskBackend(t))
	reg := output.NewRegistry(ctx, cas)
	t.Cleanup(func() { _ = reg.Close() })
	return reg, ctx
}

func TestCommandErrorMessage(t *testing.T) {
	ce := &CommandError{
		TargetLabel: label.TargetLabel{Package: "p", Name: "n"},
		ExitCode:    42,
		Output:      "bad output",
	}
	msg := ce.Error()
	if !strings.Contains(msg, "//p:n") || !strings.Contains(msg, "42") || !strings.Contains(msg, "bad output") {
		t.Fatalf("unexpected message: %s", msg)
	}
}

func TestGetBinToolPaths(t *testing.T) {
	prev := config.Global
	config.Global = config.WorkspaceConfig{WorkspaceRoot: "/ws", Root: "/ws"}
	t.Cleanup(func() { config.Global = prev })

	tool := &model.Target{
		Label:     label.TargetLabel{Package: "pkg/tool", Name: "tool"},
		BinOutput: model.NewOutput("file", "bin/tool"),
	}
	consumer := &model.Target{
		Label:        label.TargetLabel{Package: "pkg/tool", Name: "consumer"},
		Dependencies: []label.TargetLabel{tool.Label},
	}
	graph := dag.NewDirectedGraphFromTargets(consumer, tool)
	graph.AddEdge(tool, consumer)

	executor := &Executor{graph: graph}
	bins, err := executor.getBinToolPaths(consumer)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if got, want := bins[tool.Label.String()], "/ws/pkg/tool/bin/tool"; got != want {
		t.Fatalf("expected %q, got %q", want, got)
	}
	if got, ok := bins[":tool"]; !ok || got != "/ws/pkg/tool/bin/tool" {
		t.Fatalf("expected :tool shorthand, got %q (ok=%v)", got, ok)
	}
	if got, ok := bins["//pkg/tool"]; !ok || got != "/ws/pkg/tool/bin/tool" {
		t.Fatalf("expected //pkg/tool shorthand, got %q (ok=%v)", got, ok)
	}
}

func TestGetBinToolPathsSkipsTargetsWithoutBinOutput(t *testing.T) {
	prev := config.Global
	config.Global = config.WorkspaceConfig{WorkspaceRoot: "/ws", Root: "/ws"}
	t.Cleanup(func() { config.Global = prev })

	dep := &model.Target{
		Label: label.TargetLabel{Package: "p", Name: "dep"},
	}
	consumer := &model.Target{
		Label:        label.TargetLabel{Package: "p", Name: "c"},
		Dependencies: []label.TargetLabel{dep.Label},
	}
	graph := dag.NewDirectedGraphFromTargets(consumer, dep)
	graph.AddEdge(dep, consumer)

	executor := &Executor{graph: graph}
	bins, err := executor.getBinToolPaths(consumer)
	if err != nil || len(bins) != 0 {
		t.Fatalf("expected empty bin map, got %v err=%v", bins, err)
	}
}

func TestGetDependencyOutputIdentifiers(t *testing.T) {
	prev := config.Global
	config.Global = config.WorkspaceConfig{WorkspaceRoot: "/ws", Root: "/ws"}
	t.Cleanup(func() { config.Global = prev })

	dep := &model.Target{
		Label:   label.TargetLabel{Package: "pkg", Name: "dep"},
		Outputs: []model.Output{model.NewOutput(string(handlers.FileHandler), "out.txt")},
	}
	consumer := &model.Target{
		Label:        label.TargetLabel{Package: "pkg", Name: "consumer"},
		Dependencies: []label.TargetLabel{dep.Label},
	}
	graph := dag.NewDirectedGraphFromTargets(consumer, dep)
	graph.AddEdge(dep, consumer)

	executor := &Executor{graph: graph}
	got := executor.getDependencyOutputIdentifiers(consumer)
	exp := "/ws/pkg/out.txt"
	if vs, ok := got[dep.Label.String()]; !ok || vs[0] != exp {
		t.Fatalf("dep entry missing/incorrect: %v", got)
	}
	if vs, ok := got[":dep"]; !ok || vs[0] != exp {
		t.Fatalf(":dep entry missing: %v", got)
	}
}

func TestGetTargetOutputIdentifiersWithBinAndDir(t *testing.T) {
	prev := config.Global
	config.Global = config.WorkspaceConfig{WorkspaceRoot: "/ws", Root: "/ws"}
	t.Cleanup(func() { config.Global = prev })

	tgt := &model.Target{
		Label:     label.TargetLabel{Package: "p", Name: "t"},
		Outputs:   []model.Output{model.NewOutput(string(handlers.DirHandler), "out")},
		BinOutput: model.NewOutput(string(handlers.FileHandler), "bin/tool"),
	}
	ids := getTargetOutputIdentifiers(tgt)
	if len(ids) != 2 {
		t.Fatalf("expected 2 identifiers, got %v", ids)
	}
	if ids[0] != "/ws/p/out" {
		t.Fatalf("expected /ws/p/out, got %s", ids[0])
	}
	if ids[1] != "/ws/p/bin/tool" {
		t.Fatalf("expected /ws/p/bin/tool, got %s", ids[1])
	}
}

func TestGetTargetOutputIdentifiersWithOciOutput(t *testing.T) {
	tgt := &model.Target{
		Label:   label.TargetLabel{Package: "p", Name: "t"},
		Outputs: []model.Output{model.NewOutput("oci", "myrepo/image:tag")},
	}
	ids := getTargetOutputIdentifiers(tgt)
	if len(ids) != 1 || ids[0] != "myrepo/image:tag" {
		t.Fatalf("expected oci identifier as-is, got %v", ids)
	}
}

func TestGetTargetOutputIdentifiersSkipsUnsetOutputs(t *testing.T) {
	tgt := &model.Target{
		Label:   label.TargetLabel{Package: "p", Name: "t"},
		Outputs: []model.Output{{}},
	}
	ids := getTargetOutputIdentifiers(tgt)
	if ids != nil {
		t.Fatalf("expected nil, got %v", ids)
	}
}

func TestAsyncWaitTimeZeroByDefault(t *testing.T) {
	e := &Executor{}
	if e.AsyncWaitTime() != 0 {
		t.Fatal("expected zero")
	}
}

func TestExecuteRunsTargetCommand(t *testing.T) {
	prev := config.Global
	tmp := t.TempDir()
	config.Global = config.WorkspaceConfig{
		Root:                     tmp,
		WorkspaceRoot:            tmp,
		NumWorkers:               1,
		NumIOWorkers:             1,
		EnableCache:              true,
		AsyncCacheWrites:         false,
		DisableDefaultShellFlags: true,
	}
	t.Cleanup(func() { config.Global = prev })

	os.MkdirAll(filepath.Join(tmp, "pkg"), 0o755)
	os.MkdirAll(filepath.Join(tmp, "logs", "pkg"), 0o755)

	reg, ctx := newRegistry(t)
	cas := caching.NewCas(newDiskBackend(t))
	tcache := caching.NewTargetResultCache(newDiskBackend(t))
	taint := caching.NewTaintStore()
	_ = cas

	tgt := &model.Target{
		Label:      label.TargetLabel{Package: "pkg", Name: "t"},
		Command:    `echo ok`,
		IsSelected: true,
	}
	graph := dag.NewDirectedGraphFromTargets(tgt)

	executor := NewExecutor(tcache, taint, reg, graph, false, false, true, config.LoadOutputsAll)
	_, err := executor.Execute(ctx)
	if err != nil {
		t.Fatalf("Execute returned: %v", err)
	}
}

func TestExecuteCancelledContextInterrupts(t *testing.T) {
	prev := config.Global
	tmp := t.TempDir()
	config.Global = config.WorkspaceConfig{
		Root: tmp, WorkspaceRoot: tmp, NumWorkers: 1, NumIOWorkers: 1,
		EnableCache: true, DisableDefaultShellFlags: true,
	}
	t.Cleanup(func() { config.Global = prev })

	reg, _ := newRegistry(t)
	tcache := caching.NewTargetResultCache(newDiskBackend(t))
	taint := caching.NewTaintStore()
	graph := dag.NewDirectedGraph()

	executor := NewExecutor(tcache, taint, reg, graph, false, false, true, config.LoadOutputsAll)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, _ = executor.Execute(ctx)
}

func TestExecuteWithoutOutputsTarget(t *testing.T) {
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
		Command:    `echo ok`,
		IsSelected: true,
	}
	graph := dag.NewDirectedGraphFromTargets(tgt)

	executor := NewExecutor(tcache, taint, reg, graph, false, false, true, config.LoadOutputsAll)
	if _, err := executor.Execute(ctx); err != nil {
		t.Fatalf("expected success, got %v", err)
	}

	tcache2 := caching.NewTargetResultCache(newDiskBackend(t))
	taint2 := caching.NewTaintStore()
	tgt2 := &model.Target{
		Label:      label.TargetLabel{Package: "pkg", Name: "t"},
		Command:    `echo ok`,
		IsSelected: true,
		Tags:       []string{model.TagNoCache},
	}
	graph2 := dag.NewDirectedGraphFromTargets(tgt2)
	executor2 := NewExecutor(tcache2, taint2, reg, graph2, false, false, true, config.LoadOutputsAll)
	if _, err := executor2.Execute(ctx); err != nil {
		t.Fatalf("expected success no-cache target, got %v", err)
	}
}

func TestExecuteFailingTarget(t *testing.T) {
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
		Label:      label.TargetLabel{Package: "pkg", Name: "fail"},
		Command:    `exit 1`,
		IsSelected: true,
	}
	graph := dag.NewDirectedGraphFromTargets(tgt)
	executor := NewExecutor(tcache, taint, reg, graph, true, false, true, config.LoadOutputsAll)
	completion, _ := executor.Execute(ctx)
	if errs := completion.GetErrors(); len(errs) == 0 {
		t.Fatal("expected at least one error in completion map")
	}
}

func TestRunOutputChecksPasses(t *testing.T) {
	prev := config.Global
	tmp := t.TempDir()
	config.Global = config.WorkspaceConfig{
		Root: tmp, WorkspaceRoot: tmp, DisableDefaultShellFlags: true,
	}
	t.Cleanup(func() { config.Global = prev })
	os.MkdirAll(filepath.Join(tmp, "pkg"), 0o755)
	os.MkdirAll(filepath.Join(tmp, "logs", "pkg"), 0o755)

	tgt := &model.Target{
		Label:        label.TargetLabel{Package: "pkg", Name: "t"},
		OutputChecks: []model.OutputCheck{{Command: "echo hello", ExpectedOutput: "hello"}},
	}
	if err := runOutputChecks(context.Background(), tgt, nil, nil); err != nil {
		t.Fatalf("expected nil, got %v", err)
	}
}

func TestRunOutputChecksMismatch(t *testing.T) {
	prev := config.Global
	tmp := t.TempDir()
	config.Global = config.WorkspaceConfig{
		Root: tmp, WorkspaceRoot: tmp, DisableDefaultShellFlags: true,
	}
	t.Cleanup(func() { config.Global = prev })
	os.MkdirAll(filepath.Join(tmp, "pkg"), 0o755)
	os.MkdirAll(filepath.Join(tmp, "logs", "pkg"), 0o755)

	tgt := &model.Target{
		Label:        label.TargetLabel{Package: "pkg", Name: "t"},
		OutputChecks: []model.OutputCheck{{Command: "echo wrong", ExpectedOutput: "hello"}},
	}
	if err := runOutputChecks(context.Background(), tgt, nil, nil); err == nil {
		t.Fatal("expected mismatch error")
	}
}

func TestRunOutputChecksCommandFails(t *testing.T) {
	prev := config.Global
	tmp := t.TempDir()
	config.Global = config.WorkspaceConfig{
		Root: tmp, WorkspaceRoot: tmp, DisableDefaultShellFlags: true,
	}
	t.Cleanup(func() { config.Global = prev })
	os.MkdirAll(filepath.Join(tmp, "pkg"), 0o755)
	os.MkdirAll(filepath.Join(tmp, "logs", "pkg"), 0o755)

	tgt := &model.Target{
		Label:        label.TargetLabel{Package: "pkg", Name: "t"},
		OutputChecks: []model.OutputCheck{{Command: "exit 7", ExpectedOutput: "ok"}},
	}
	if err := runOutputChecks(context.Background(), tgt, nil, nil); err == nil {
		t.Fatal("expected command error")
	}
}

func TestRunOutputChecksAllowsBlankExpectation(t *testing.T) {
	prev := config.Global
	tmp := t.TempDir()
	config.Global = config.WorkspaceConfig{
		Root: tmp, WorkspaceRoot: tmp, DisableDefaultShellFlags: true,
	}
	t.Cleanup(func() { config.Global = prev })
	os.MkdirAll(filepath.Join(tmp, "pkg"), 0o755)
	os.MkdirAll(filepath.Join(tmp, "logs", "pkg"), 0o755)

	tgt := &model.Target{
		Label:        label.TargetLabel{Package: "pkg", Name: "t"},
		OutputChecks: []model.OutputCheck{{Command: "echo anything"}},
	}
	if err := runOutputChecks(context.Background(), tgt, nil, nil); err != nil {
		t.Fatalf("expected nil with blank expected, got %v", err)
	}
}

func TestPoolCoordinatorShutdown(t *testing.T) {
	logger := console.NewFromSugared(zaptest.NewLogger(t).Sugar(), zapcore.DebugLevel)
	pc := NewPoolCoordinator(logger, 1, 1, func(tea.Msg) {}, 0)
	pc.StartTaskWorkers(context.Background())
	pc.StartIOWorkers(context.Background())
	pc.Shutdown()
}

func TestLogTargetBuiltAndCachedNoOpWithoutLogger(t *testing.T) {
	ctx := context.Background()
	logger := console.NewFromSugared(zaptest.NewLogger(t).Sugar(), zapcore.DebugLevel)
	tgt := &model.Target{Label: label.TargetLabel{Package: "p", Name: "t"}}
	logTargetBuilt(ctx, logger, tgt, 1.5)
	logTargetCached(ctx, logger, tgt, 1.5)
}

func TestLogTargetBuiltAndCachedWithResultLogger(t *testing.T) {
	logger := console.NewFromSugared(zaptest.NewLogger(t).Sugar(), zapcore.DebugLevel)
	rl := console.NewResultLogger([]string{"//p:t", "//p:t_test"}, 80)
	ctx := context.WithValue(context.Background(), console.ResultLoggerKey{}, rl)
	tgt := &model.Target{Label: label.TargetLabel{Package: "p", Name: "t"}}
	logTargetBuilt(ctx, logger, tgt, 1.5)
	logTargetCached(ctx, logger, tgt, 1.5)
	tgt2 := &model.Target{Label: label.TargetLabel{Package: "p", Name: "t_test"}}
	logTargetBuilt(ctx, logger, tgt2, 0.1)
	logTargetCached(ctx, logger, tgt2, 0.1)
}

func TestExecutorWithoutCacheRunsTarget(t *testing.T) {
	prev := config.Global
	tmp := t.TempDir()
	config.Global = config.WorkspaceConfig{
		Root: tmp, WorkspaceRoot: tmp, NumWorkers: 1, NumIOWorkers: 1,
		EnableCache: false, DisableDefaultShellFlags: true,
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
	executor := NewExecutor(tcache, taint, reg, graph, false, false, false, config.LoadOutputsAll)
	if _, err := executor.Execute(ctx); err != nil {
		t.Fatalf("expected success, got %v", err)
	}
}

func TestExecuteWithExtraArgs(t *testing.T) {
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
	ctx = WithExtraArgs(ctx, []string{"-k", "foo"})
	tcache := caching.NewTargetResultCache(newDiskBackend(t))
	taint := caching.NewTaintStore()

	tgt := &model.Target{
		Label:      label.TargetLabel{Package: "pkg", Name: "t"},
		Command:    `echo "$@"`,
		IsSelected: true,
	}
	graph := dag.NewDirectedGraphFromTargets(tgt)
	executor := NewExecutor(tcache, taint, reg, graph, false, false, true, config.LoadOutputsAll)
	if _, err := executor.Execute(ctx); err != nil {
		t.Fatalf("expected success, got %v", err)
	}
}

func TestGetExtendedTargetEnvIncludesGrogVars(t *testing.T) {
	prev := config.Global
	config.Global = config.WorkspaceConfig{
		WorkspaceRoot: "/ws",
		Root:          "/ws",
		OS:            "linux",
		Arch:          "amd64",
		PlatformTags:  []string{"a", "b"},
		EnvironmentVariables: map[string]string{
			"GLOBAL_VAR": "global-val",
		},
	}
	t.Cleanup(func() { config.Global = prev })

	tgt := &model.Target{
		Label: label.TargetLabel{Package: "p", Name: "t"},
		EnvironmentVariables: map[string]string{
			"TARGET_VAR": "target-val",
		},
	}
	env := GetExtendedTargetEnv(context.Background(), tgt)
	joined := strings.Join(env, "\n")
	mustContain := []string{
		"GROG_TARGET=//p:t",
		"GROG_OS=linux",
		"GROG_ARCH=amd64",
		"GROG_PACKAGE=p",
		"GROG_WORKSPACE_ROOT=/ws",
		"GLOBAL_VAR=global-val",
		"TARGET_VAR=target-val",
	}
	for _, want := range mustContain {
		if !strings.Contains(joined, want) {
			t.Errorf("missing env entry %q", want)
		}
	}
}

func TestCacheWriterPersistNilPreparedTarget(t *testing.T) {
	backend := &recordingCacheBackend{}
	targetCache := caching.NewTargetResultCache(backend)
	cw, pool := newTestCacheWriter(t, targetCache, false, context.Background())
	defer pool.Shutdown()
	if err := cw.PersistPreparedTarget(context.Background(), "//pkg:t", nil, func(worker.StatusUpdate) {}); err == nil {
		t.Fatal("expected error for nil prepared target")
	}
}

func TestCacheWriterSyncEmptyPlans(t *testing.T) {
	backend := &recordingCacheBackend{}
	targetCache := caching.NewTargetResultCache(backend)
	cw, pool := newTestCacheWriter(t, targetCache, false, context.Background())
	defer pool.Shutdown()

	pt := &output.PreparedTargetResult{
		TargetResult: &gen.TargetResult{ChangeHash: "x"},
	}
	if err := cw.PersistPreparedTarget(context.Background(), "//p:t", pt, func(worker.StatusUpdate) {}); err != nil {
		t.Fatalf("expected nil err, got %v", err)
	}
	if calls := backend.Calls(); len(calls) != 1 {
		t.Fatalf("expected one target-cache call, got %v", calls)
	}
}

func TestCacheWriterSyncWritePlanFailureIsFatal(t *testing.T) {
	backend := &recordingCacheBackend{}
	targetCache := caching.NewTargetResultCache(backend)
	cw, pool := newTestCacheWriter(t, targetCache, false, context.Background())
	defer pool.Shutdown()

	plan := &mockWritePlan{uploadErr: os.ErrInvalid}
	pt := &output.PreparedTargetResult{
		TargetResult: &gen.TargetResult{ChangeHash: "x"},
		WritePlans:   []handlers.OutputWritePlan{plan},
	}
	if err := cw.PersistPreparedTarget(context.Background(), "//p:t", pt, func(worker.StatusUpdate) {}); err == nil {
		t.Fatal("expected fatal sync error")
	}
}

func TestCacheWriterHasPendingWrites(t *testing.T) {
	backend := &recordingCacheBackend{}
	targetCache := caching.NewTargetResultCache(backend)
	cw, pool := newTestCacheWriter(t, targetCache, true, context.Background())
	defer pool.Shutdown()

	if cw.HasPendingWrites() {
		t.Fatal("expected no pending writes initially")
	}
	plan := &mockWritePlan{}
	pt := &output.PreparedTargetResult{
		TargetResult: &gen.TargetResult{ChangeHash: "x"},
		WritePlans:   []handlers.OutputWritePlan{plan},
	}
	if err := cw.PersistPreparedTarget(context.Background(), "//p:t", pt, func(worker.StatusUpdate) {}); err != nil {
		t.Fatal(err)
	}
	cw.Wait()
	if cw.HasPendingWrites() {
		t.Fatal("expected no pending writes after Wait")
	}
}

func TestExecutorWaitForAsyncWritesNoOpWhenCancelled(t *testing.T) {
	e := &Executor{}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	e.WaitForAsyncWrites(ctx)
}

func TestExecutorWaitForAsyncWritesNoOpWhenAlreadyDrained(t *testing.T) {
	e := &Executor{asyncDrained: true}
	e.WaitForAsyncWrites(context.Background())
}
