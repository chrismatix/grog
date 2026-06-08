package execution

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"grog/internal/caching"
	"grog/internal/caching/backends"
	"grog/internal/config"
	"grog/internal/dag"
	"grog/internal/label"
	"grog/internal/model"
	"grog/internal/output"
	"grog/internal/output/handlers"
	"grog/internal/proto/gen"
	"grog/internal/worker"
)

// ---------------------------------------------------------------------------
// getTransitiveOutputsByTag tests
// ---------------------------------------------------------------------------

// TestGetTransitiveOutputsByTagReturnsOutputsFromTaggedAncestors builds a
// diamond graph:
//
//	A (tag: find-links, output: dist/a.whl)
//	├── B (no tag, output: dist/b.tar)
//	│   └── D (tag: find-links, output: dist/d.whl)
//	└── C (tag: find-links, output: dist/c.whl)
//	    └── D
//
// Querying outputs_by_tag("find-links") from the perspective of a target
// that depends on A should return outputs from A, C, and D (all tagged
// ancestors) but NOT B.
func TestGetTransitiveOutputsByTagReturnsOutputsFromTaggedAncestors(t *testing.T) {
	prev := config.Global
	config.Global = config.WorkspaceConfig{WorkspaceRoot: "/workspace"}
	t.Cleanup(func() { config.Global = prev })

	d := &model.Target{
		Label:   label.TargetLabel{Package: "pkg", Name: "d"},
		Tags:    []string{"find-links"},
		Outputs: []model.Output{model.NewOutput("dir", "dist/d")},
	}
	c := &model.Target{
		Label:        label.TargetLabel{Package: "pkg", Name: "c"},
		Tags:         []string{"find-links"},
		Outputs:      []model.Output{model.NewOutput("dir", "dist/c")},
		Dependencies: []label.TargetLabel{d.Label},
	}
	b := &model.Target{
		Label:        label.TargetLabel{Package: "pkg", Name: "b"},
		Outputs:      []model.Output{model.NewOutput("file", "dist/b.tar")},
		Dependencies: []label.TargetLabel{d.Label},
	}
	a := &model.Target{
		Label:        label.TargetLabel{Package: "pkg", Name: "a"},
		Tags:         []string{"find-links"},
		Outputs:      []model.Output{model.NewOutput("dir", "dist/a")},
		Dependencies: []label.TargetLabel{b.Label, c.Label},
	}
	root := &model.Target{
		Label:        label.TargetLabel{Package: "pkg", Name: "root"},
		Dependencies: []label.TargetLabel{a.Label},
	}

	graph := dag.NewDirectedGraphFromTargets(root, a, b, c, d)
	// Edges point from dependency → dependant.
	// AddEdge(dep, dependant) means dependant depends on dep.
	graph.AddEdge(a, root)
	graph.AddEdge(b, a)
	graph.AddEdge(c, a)
	graph.AddEdge(d, b)
	graph.AddEdge(d, c)

	executor := &Executor{graph: graph}
	result := executor.getTransitiveOutputsByTag(root)

	findLinks, ok := result["find-links"]
	if !ok {
		t.Fatal("expected 'find-links' key in transitive tagged outputs")
	}

	// Should contain outputs from a, c, d (all tagged "find-links") but not b
	expected := map[string]bool{
		"/workspace/pkg/dist/a": true,
		"/workspace/pkg/dist/c": true,
		"/workspace/pkg/dist/d": true,
	}
	got := make(map[string]bool, len(findLinks))
	for _, path := range findLinks {
		got[path] = true
	}

	for exp := range expected {
		if !got[exp] {
			t.Errorf("expected output %q in find-links, got %v", exp, findLinks)
		}
	}

	// b's output should NOT be present
	for _, path := range findLinks {
		if path == "/workspace/pkg/dist/b.tar" {
			t.Errorf("b's output should not appear — b has no find-links tag, got %v", findLinks)
		}
	}
}

// TestGetTransitiveOutputsByTagDeduplicatesDiamondOutputs verifies that when
// the same ancestor is reachable via multiple paths (diamond), its outputs
// appear only once in the result.
func TestGetTransitiveOutputsByTagDeduplicatesDiamondOutputs(t *testing.T) {
	prev := config.Global
	config.Global = config.WorkspaceConfig{WorkspaceRoot: "/workspace"}
	t.Cleanup(func() { config.Global = prev })

	shared := &model.Target{
		Label:   label.TargetLabel{Package: "lib", Name: "shared"},
		Tags:    []string{"wheels"},
		Outputs: []model.Output{model.NewOutput("dir", "dist")},
	}
	left := &model.Target{
		Label:        label.TargetLabel{Package: "lib", Name: "left"},
		Dependencies: []label.TargetLabel{shared.Label},
	}
	right := &model.Target{
		Label:        label.TargetLabel{Package: "lib", Name: "right"},
		Dependencies: []label.TargetLabel{shared.Label},
	}
	top := &model.Target{
		Label:        label.TargetLabel{Package: "lib", Name: "top"},
		Dependencies: []label.TargetLabel{left.Label, right.Label},
	}

	graph := dag.NewDirectedGraphFromTargets(top, left, right, shared)
	graph.AddEdge(left, top)
	graph.AddEdge(right, top)
	graph.AddEdge(shared, left)
	graph.AddEdge(shared, right)

	executor := &Executor{graph: graph}
	result := executor.getTransitiveOutputsByTag(top)

	wheels := result["wheels"]
	if len(wheels) != 1 {
		t.Fatalf("expected 1 deduplicated output for 'wheels', got %d: %v", len(wheels), wheels)
	}
	if wheels[0] != "/workspace/lib/dist" {
		t.Errorf("expected /workspace/lib/dist, got %s", wheels[0])
	}
}

// TestGetTransitiveOutputsByTagMultipleTags verifies that outputs are correctly
// bucketed when ancestors have different tags.
func TestGetTransitiveOutputsByTagMultipleTags(t *testing.T) {
	prev := config.Global
	config.Global = config.WorkspaceConfig{WorkspaceRoot: "/ws"}
	t.Cleanup(func() { config.Global = prev })

	depA := &model.Target{
		Label:   label.TargetLabel{Package: "x", Name: "a"},
		Tags:    []string{"find-links", "python"},
		Outputs: []model.Output{model.NewOutput("dir", "dist")},
	}
	depB := &model.Target{
		Label:   label.TargetLabel{Package: "y", Name: "b"},
		Tags:    []string{"python"},
		Outputs: []model.Output{model.NewOutput("file", "lib.so")},
	}
	root := &model.Target{
		Label:        label.TargetLabel{Package: "z", Name: "root"},
		Dependencies: []label.TargetLabel{depA.Label, depB.Label},
	}

	graph := dag.NewDirectedGraphFromTargets(root, depA, depB)
	graph.AddEdge(depA, root)
	graph.AddEdge(depB, root)

	executor := &Executor{graph: graph}
	result := executor.getTransitiveOutputsByTag(root)

	// "find-links" should only have depA's output
	if fl, ok := result["find-links"]; !ok || len(fl) != 1 {
		t.Errorf("expected 1 find-links output, got %v", result["find-links"])
	}

	// "python" should have both depA and depB outputs
	py := result["python"]
	if len(py) != 2 {
		t.Errorf("expected 2 python outputs, got %d: %v", len(py), py)
	}
}

// TestGetTransitiveOutputsByTagNoTaggedAncestors returns an empty map when no
// ancestors have tags.
func TestGetTransitiveOutputsByTagNoTaggedAncestors(t *testing.T) {
	dep := &model.Target{
		Label:   label.TargetLabel{Package: "pkg", Name: "dep"},
		Outputs: []model.Output{model.NewOutput("file", "out.txt")},
	}
	root := &model.Target{
		Label:        label.TargetLabel{Package: "pkg", Name: "root"},
		Dependencies: []label.TargetLabel{dep.Label},
	}

	graph := dag.NewDirectedGraphFromTargets(root, dep)
	graph.AddEdge(dep, root)

	executor := &Executor{graph: graph}
	result := executor.getTransitiveOutputsByTag(root)

	if len(result) != 0 {
		t.Errorf("expected empty map for untagged ancestors, got %v", result)
	}
}

// TestGetTransitiveOutputsByTagSkipsAncestorsWithNoOutputs ensures that tagged
// ancestors that produce no outputs don't create empty entries.
func TestGetTransitiveOutputsByTagSkipsAncestorsWithNoOutputs(t *testing.T) {
	// A tagged target with no outputs (aggregator/alias)
	aggregator := &model.Target{
		Label: label.TargetLabel{Package: "pkg", Name: "agg"},
		Tags:  []string{"find-links"},
		// No outputs
	}
	root := &model.Target{
		Label:        label.TargetLabel{Package: "pkg", Name: "root"},
		Dependencies: []label.TargetLabel{aggregator.Label},
	}

	graph := dag.NewDirectedGraphFromTargets(root, aggregator)
	graph.AddEdge(aggregator, root)

	executor := &Executor{graph: graph}
	result := executor.getTransitiveOutputsByTag(root)

	// Aggregator has the tag but no outputs — should not produce an entry
	if fl, ok := result["find-links"]; ok && len(fl) > 0 {
		t.Errorf("expected no find-links outputs for tagless aggregator, got %v", fl)
	}
}

// ---------------------------------------------------------------------------
// getTransitiveOutputs tests
// ---------------------------------------------------------------------------

// TestGetTransitiveOutputsReturnsAllAncestorOutputs verifies that
// getTransitiveOutputs collects outputs from all ancestors regardless of tags.
func TestGetTransitiveOutputsReturnsAllAncestorOutputs(t *testing.T) {
	prev := config.Global
	config.Global = config.WorkspaceConfig{WorkspaceRoot: "/workspace"}
	t.Cleanup(func() { config.Global = prev })

	tagged := &model.Target{
		Label:   label.TargetLabel{Package: "pkg", Name: "tagged"},
		Tags:    []string{"find-links"},
		Outputs: []model.Output{model.NewOutput("dir", "dist/tagged")},
	}
	untagged := &model.Target{
		Label:   label.TargetLabel{Package: "pkg", Name: "untagged"},
		Outputs: []model.Output{model.NewOutput("file", "out.txt")},
	}
	root := &model.Target{
		Label:        label.TargetLabel{Package: "pkg", Name: "root"},
		Dependencies: []label.TargetLabel{tagged.Label, untagged.Label},
	}

	graph := dag.NewDirectedGraphFromTargets(root, tagged, untagged)
	graph.AddEdge(tagged, root)
	graph.AddEdge(untagged, root)

	executor := &Executor{graph: graph}
	result := executor.getTransitiveOutputs(root)

	// Should contain outputs from both tagged and untagged ancestors
	expected := map[string]bool{
		"/workspace/pkg/dist/tagged": true,
		"/workspace/pkg/out.txt":     true,
	}
	got := make(map[string]bool, len(result))
	for _, path := range result {
		got[path] = true
	}
	for exp := range expected {
		if !got[exp] {
			t.Errorf("expected output %q in transitive outputs, got %v", exp, result)
		}
	}
	if len(result) != 2 {
		t.Errorf("expected 2 outputs, got %d: %v", len(result), result)
	}
}

// TestGetTransitiveOutputsDeduplicatesDiamond verifies that diamond-reachable
// ancestors produce deduplicated output lists.
func TestGetTransitiveOutputsDeduplicatesDiamond(t *testing.T) {
	prev := config.Global
	config.Global = config.WorkspaceConfig{WorkspaceRoot: "/ws"}
	t.Cleanup(func() { config.Global = prev })

	shared := &model.Target{
		Label:   label.TargetLabel{Package: "lib", Name: "shared"},
		Outputs: []model.Output{model.NewOutput("dir", "dist")},
	}
	left := &model.Target{
		Label:        label.TargetLabel{Package: "lib", Name: "left"},
		Dependencies: []label.TargetLabel{shared.Label},
	}
	right := &model.Target{
		Label:        label.TargetLabel{Package: "lib", Name: "right"},
		Dependencies: []label.TargetLabel{shared.Label},
	}
	top := &model.Target{
		Label:        label.TargetLabel{Package: "lib", Name: "top"},
		Dependencies: []label.TargetLabel{left.Label, right.Label},
	}

	graph := dag.NewDirectedGraphFromTargets(top, left, right, shared)
	graph.AddEdge(left, top)
	graph.AddEdge(right, top)
	graph.AddEdge(shared, left)
	graph.AddEdge(shared, right)

	executor := &Executor{graph: graph}
	result := executor.getTransitiveOutputs(top)

	if len(result) != 1 {
		t.Fatalf("expected 1 deduplicated output, got %d: %v", len(result), result)
	}
	if result[0] != "/ws/lib/dist" {
		t.Errorf("expected /ws/lib/dist, got %s", result[0])
	}
}

// TestGetTransitiveOutputsEmptyForNoAncestors returns nil when there are no
// transitive dependencies.
func TestGetTransitiveOutputsEmptyForNoAncestors(t *testing.T) {
	root := &model.Target{
		Label: label.TargetLabel{Package: "pkg", Name: "root"},
	}

	graph := dag.NewDirectedGraphFromTargets(root)
	executor := &Executor{graph: graph}
	result := executor.getTransitiveOutputs(root)

	if len(result) != 0 {
		t.Errorf("expected empty slice for target with no ancestors, got %v", result)
	}
}

// countingTargetBackend records every Get on the "target" namespace so we can
// assert LoadDependencyOutputs never even consulted the cache for a dep that
// finished building earlier in this session.
type countingTargetBackend struct {
	getCalls atomic.Int64
}

func (c *countingTargetBackend) TypeName() string { return "counting" }

func (c *countingTargetBackend) Get(_ context.Context, path, _ string) (io.ReadCloser, error) {
	if path == "target" {
		c.getCalls.Add(1)
	}
	return io.NopCloser(bytes.NewReader(nil)), errors.New("not found")
}

func (c *countingTargetBackend) Set(context.Context, string, string, io.Reader) error { return nil }
func (c *countingTargetBackend) Delete(context.Context, string, string) error         { return nil }
func (c *countingTargetBackend) Exists(context.Context, string, string) (bool, error) {
	return false, nil
}
func (c *countingTargetBackend) Size(context.Context, string, string) (int64, error) { return 0, nil }
func (c *countingTargetBackend) BeginWrite(context.Context) (backends.StagedWriter, error) {
	return nil, errors.New("BeginWrite not implemented")
}
func (c *countingTargetBackend) ListKeys(context.Context, string, string) ([]string, error) {
	return nil, nil
}

// TestLoadDependencyOutputsSkipsAlreadyLoadedDeps is the regression test for
// the rate-limit-burst bug: when a dep's OnTargetComplete has already set
// OutputsLoaded=true (synchronously, before the async cache write was even
// scheduled), downstream targets calling LoadDependencyOutputs must NOT
// attempt a targetCache.Load and must NOT call rerunDependency. Otherwise
// every downstream re-executes the same dep, racing on the Docker loopback
// tag (Cleanup removes the tag mid-push) and the GCS target-cache object
// (concurrent writes trip the 429 mutation limit).
func TestLoadDependencyOutputsSkipsAlreadyLoadedDeps(t *testing.T) {
	prev := config.Global
	config.Global = config.WorkspaceConfig{WorkspaceRoot: "/ws"}
	t.Cleanup(func() { config.Global = prev })

	dep := &model.Target{
		Label:         label.TargetLabel{Package: "pkg", Name: "dep"},
		ChangeHash:    "dep-hash",
		OutputsLoaded: true,
	}
	root := &model.Target{
		Label:        label.TargetLabel{Package: "pkg", Name: "root"},
		Dependencies: []label.TargetLabel{dep.Label},
	}

	graph := dag.NewDirectedGraphFromTargets(root, dep)
	graph.AddEdge(dep, root)

	backend := &countingTargetBackend{}
	executor := &Executor{
		graph:       graph,
		targetCache: caching.NewTargetResultCache(backend),
	}

	if err := executor.LoadDependencyOutputs(context.Background(), root, func(worker.StatusUpdate) {}); err != nil {
		t.Fatalf("LoadDependencyOutputs returned error: %v", err)
	}
	if got := backend.getCalls.Load(); got != 0 {
		t.Fatalf("expected 0 target-cache Get calls for an already-loaded dep, got %d", got)
	}
}

// TestLoadDependencyOutputsDedupsConcurrentReruns verifies that when several
// downstream targets concurrently fall into the rerun branch for the same
// dep — because its target cache hasn't landed yet — only one rerun fires.
// Without dedup each downstream submits its own async cache write, producing
// the 429 burst the bug report shows.
func TestLoadDependencyOutputsDedupsConcurrentReruns(t *testing.T) {
	prev := config.Global
	config.Global = config.WorkspaceConfig{WorkspaceRoot: "/ws"}
	t.Cleanup(func() { config.Global = prev })

	depLabel := label.TargetLabel{Package: "pkg", Name: "dep"}
	executor := &Executor{}

	var rerunCount atomic.Int64
	firstInFlight := make(chan struct{})
	releaseFirst := make(chan struct{})
	var wg sync.WaitGroup

	wg.Add(1)
	go func() {
		defer wg.Done()
		_, _, _ = executor.rerunGroup.Do(depLabel.String(), func() (any, error) {
			rerunCount.Add(1)
			close(firstInFlight)
			<-releaseFirst
			return nil, nil
		})
	}()

	<-firstInFlight

	const followers = 4
	for i := 0; i < followers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _, _ = executor.rerunGroup.Do(depLabel.String(), func() (any, error) {
				rerunCount.Add(1)
				return nil, nil
			})
		}()
	}

	// Give followers time to register as waiters on the in-flight call before
	// we release it; otherwise a follower may not enter Do until after the
	// first call completes, in which case singleflight starts a fresh call
	// and the dedup we want to assert never gets exercised.
	time.Sleep(50 * time.Millisecond)
	close(releaseFirst)
	wg.Wait()

	if got := rerunCount.Load(); got != 1 {
		t.Fatalf("expected exactly 1 rerun for %d concurrent callers, got %d", followers+1, got)
	}
}

func TestFormatTargetResultForDebugWithNilTargetResult(t *testing.T) {
	formattedTargetResult := formatTargetResultForDebug(nil)
	if formattedTargetResult != "<nil>" {
		t.Fatalf("expected <nil> for nil target result but got %q", formattedTargetResult)
	}
}

func TestFormatTargetResultForDebugIncludesEmptyOutputsArray(t *testing.T) {
	targetResult := &gen.TargetResult{
		ChangeHash: "abc123",
	}

	formattedTargetResult := formatTargetResultForDebug(targetResult)

	if !strings.Contains(formattedTargetResult, "\"outputs\":[]") {
		t.Fatalf("expected formatted target result to include an empty outputs array but got %q", formattedTargetResult)
	}
}

// TestExecutorDeferAsyncWaitKeepsIOPoolAliveAfterExecute is a regression test for
// a bug where Execute's deferred pool shutdown closed the I/O worker pool before
// post-build work (e.g. `grog run` with load_outputs=minimal running
// LoadDependencyOutputs → re-running a dependency) could submit additional cache
// writes. The symptom was:
//
//	FATAL: could not load dependencies: build completed but failed to write
//	outputs to cache for target //...: worker pool is closed
//
// When DeferAsyncWait is set, the I/O pool must stay alive across Execute's
// return so callers can still submit writes via the executor's cacheWriter;
// it's finally drained and shut down by WaitForAsyncWrites.
func TestExecutorDeferAsyncWaitKeepsIOPoolAliveAfterExecute(t *testing.T) {
	prev := config.Global
	config.Global = config.WorkspaceConfig{
		NumWorkers:       1,
		NumIOWorkers:     1,
		AsyncCacheWrites: true,
		EnableCache:      true,
	}
	t.Cleanup(func() { config.Global = prev })

	ctx := context.Background()

	backend := &recordingCacheBackend{}
	targetCache := caching.NewTargetResultCache(backend)
	cas := caching.NewCas(backend)
	taintCache := caching.NewTaintStore()
	registry := output.NewRegistry(ctx, cas)
	graph := dag.NewDirectedGraph()

	executor := NewExecutor(
		targetCache,
		taintCache,
		registry,
		graph,
		false,
		false,
		true,
		config.LoadOutputsAll,
	)
	executor.DeferAsyncWait()

	if _, err := executor.Execute(ctx); err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}

	// Simulate the post-build write that `grog run` triggers when
	// LoadDependencyOutputs re-runs a dependency whose async cache write
	// hasn't yet landed. Before the fix this would fail with
	// "worker pool is closed".
	writePlan := &mockWritePlan{}
	preparedTarget := &output.PreparedTargetResult{
		TargetResult: &gen.TargetResult{ChangeHash: "post-build"},
		WritePlans:   []handlers.OutputWritePlan{writePlan},
	}

	if err := executor.cacheWriter.PersistPreparedTarget(
		ctx,
		"//post:build",
		preparedTarget,
		func(worker.StatusUpdate) {},
	); err != nil {
		t.Fatalf("expected post-build cache write to succeed, got: %v", err)
	}

	executor.WaitForAsyncWrites(ctx)

	if calls := writePlan.Calls(); len(calls) != 2 || calls[0] != "upload" || calls[1] != "cleanup" {
		t.Fatalf("expected upload followed by cleanup, got %v", calls)
	}
	if calls := backend.Calls(); len(calls) != 1 || calls[0] != "target" {
		t.Fatalf("expected target-cache publication after successful upload, got %v", calls)
	}
}
