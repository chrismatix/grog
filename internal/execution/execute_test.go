package execution

import (
	"context"
	"strings"
	"testing"

	"grog/internal/caching"
	"grog/internal/config"
	"grog/internal/dag"
	"grog/internal/output"
	"grog/internal/output/handlers"
	"grog/internal/proto/gen"
	"grog/internal/worker"
)

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
