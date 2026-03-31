package tracing

import (
	"fmt"
	"testing"
	"time"

	"grog/internal/dag"
	"grog/internal/label"
	"grog/internal/model"
)

func TestTraceCollector_Finalize(t *testing.T) {
	pattern, err := label.ParseTargetPattern("", "//...")
	if err != nil {
		t.Fatalf("failed to parse pattern: %v", err)
	}
	patterns := []label.TargetPattern{pattern}

	collector := NewTraceCollector("build", patterns, "0.1.0")
	collector.startTime = time.Now().Add(-5 * time.Second)

	targetA := &model.Target{
		Label:           label.TargetLabel{Package: "pkg", Name: "a"},
		Command:         "echo hello",
		IsSelected:      true,
		ChangeHash:      "hash-a",
		OutputHash:      "out-a",
		HasCacheHit:     false,
		StartTime:       collector.startTime.Add(100 * time.Millisecond),
		QueueWait:       50 * time.Millisecond,
		HashDuration:    20 * time.Millisecond,
		CacheCheckTime:  30 * time.Millisecond,
		ExecutionTime:   2 * time.Second,
		OutputWriteTime: 100 * time.Millisecond,
	}

	targetB := &model.Target{
		Label:          label.TargetLabel{Package: "pkg", Name: "b"},
		Command:        "echo world",
		IsSelected:     true,
		ChangeHash:     "hash-b",
		OutputHash:     "out-b",
		HasCacheHit:    true,
		Tags:           []string{"testonly"},
		StartTime:      collector.startTime.Add(200 * time.Millisecond),
		QueueWait:      10 * time.Millisecond,
		HashDuration:   5 * time.Millisecond,
		CacheCheckTime: 15 * time.Millisecond,
		OutputLoadTime: 50 * time.Millisecond,
	}

	graph := dag.NewDirectedGraphFromTargets(targetA, targetB)

	completionMap := dag.CompletionMap{
		targetA.Label: dag.Completion{
			IsSuccess:   true,
			NodeType:    model.TargetNode,
			CacheResult: dag.CacheMiss,
		},
		targetB.Label: dag.Completion{
			IsSuccess:   true,
			NodeType:    model.TargetNode,
			CacheResult: dag.CacheHit,
		},
	}

	trace := collector.Finalize(completionMap, graph, 200*time.Millisecond)

	if trace.Build.TraceID == "" {
		t.Error("expected non-empty trace ID")
	}
	if trace.Build.Command != "build" {
		t.Errorf("expected command 'build', got %s", trace.Build.Command)
	}
	if trace.Build.GrogVersion != "0.1.0" {
		t.Errorf("expected version '0.1.0', got %s", trace.Build.GrogVersion)
	}
	if trace.Build.AsyncCacheWaitMillis != 200 {
		t.Errorf("expected 200ms async wait, got %d", trace.Build.AsyncCacheWaitMillis)
	}
	if trace.Build.TotalTargets != 2 {
		t.Errorf("expected 2 total targets, got %d", trace.Build.TotalTargets)
	}
	if trace.Build.SuccessCount != 2 {
		t.Errorf("expected 2 successes, got %d", trace.Build.SuccessCount)
	}
	if trace.Build.CacheHitCount != 1 {
		t.Errorf("expected 1 cache hit, got %d", trace.Build.CacheHitCount)
	}
	if len(trace.Spans) != 2 {
		t.Fatalf("expected 2 spans, got %d", len(trace.Spans))
	}

	spanByLabel := make(map[string]SpanRow)
	for _, s := range trace.Spans {
		spanByLabel[s.Label] = s
	}

	spanA := spanByLabel["//pkg:a"]
	spanB := spanByLabel["//pkg:b"]

	// Span A: cache miss
	if spanA.CacheResult != "CACHE_MISS" {
		t.Errorf("expected CACHE_MISS for A, got %s", spanA.CacheResult)
	}
	if spanA.CommandDurationMillis != 2000 {
		t.Errorf("expected 2000ms command for A, got %d", spanA.CommandDurationMillis)
	}
	if spanA.QueueWaitMillis != 50 {
		t.Errorf("expected 50ms queue for A, got %d", spanA.QueueWaitMillis)
	}
	if spanA.OutputWriteMillis != 100 {
		t.Errorf("expected 100ms output write for A, got %d", spanA.OutputWriteMillis)
	}
	if spanA.ChangeHash != "hash-a" {
		t.Errorf("expected hash-a, got %s", spanA.ChangeHash)
	}

	// Span B: cache hit
	if spanB.CacheResult != "CACHE_HIT" {
		t.Errorf("expected CACHE_HIT for B, got %s", spanB.CacheResult)
	}
	if spanB.OutputLoadMillis != 50 {
		t.Errorf("expected 50ms output load for B, got %d", spanB.OutputLoadMillis)
	}
	if spanB.CommandDurationMillis != 0 {
		t.Errorf("expected 0ms command for B (cache hit), got %d", spanB.CommandDurationMillis)
	}
}

func TestTraceCollector_FailedTarget(t *testing.T) {
	collector := NewTraceCollector("test", nil, "0.2.0")
	collector.startTime = time.Now().Add(-2 * time.Second)

	target := &model.Target{
		Label:         label.TargetLabel{Package: "pkg", Name: "failing_test"},
		Command:       "exit 1",
		IsSelected:    true,
		StartTime:     collector.startTime.Add(50 * time.Millisecond),
		ExecutionTime: 500 * time.Millisecond,
	}

	graph := dag.NewDirectedGraphFromTargets(target)

	completionMap := dag.CompletionMap{
		target.Label: dag.Completion{
			IsSuccess:   false,
			NodeType:    model.TargetNode,
			CacheResult: dag.CacheMiss,
			Err:         fmt.Errorf("exit code 1"),
		},
	}

	trace := collector.Finalize(completionMap, graph, 0)

	if trace.Build.FailureCount != 1 {
		t.Errorf("expected 1 failure, got %d", trace.Build.FailureCount)
	}
	if trace.Build.SuccessCount != 0 {
		t.Errorf("expected 0 successes, got %d", trace.Build.SuccessCount)
	}
	if len(trace.Spans) != 1 {
		t.Fatalf("expected 1 span, got %d", len(trace.Spans))
	}
	if trace.Spans[0].Status != "FAILURE" {
		t.Errorf("expected FAILURE status, got %s", trace.Spans[0].Status)
	}
}
