package tracing

import (
	"context"
	"testing"
	"time"

	"grog/internal/caching/backends"
	gen "grog/internal/proto/gen"
)

func newTestStore(t *testing.T) *TraceStore {
	t.Helper()
	fs := backends.NewFileSystemCacheForTest(t.TempDir(), t.TempDir())
	return NewTraceStore(fs)
}

func makeTrace(id string, startMillis int64, command string) *gen.BuildTrace {
	return &gen.BuildTrace{
		TraceId:             id,
		Workspace:           "test-workspace",
		Command:             command,
		StartTimeUnixMillis: startMillis,
		TotalDurationMillis: 5000,
		TotalTargets:        10,
		SuccessCount:        8,
		FailureCount:        2,
		CacheHitCount:       6,
		GitCommit:           "abc1234",
		Spans: []*gen.TargetSpan{
			{
				Label:                 "//pkg:target",
				Package:               "pkg",
				Status:                gen.TargetSpan_SUCCESS,
				CacheResult:           gen.TargetSpan_CACHE_MISS,
				StartTimeUnixMillis:   startMillis + 100,
				EndTimeUnixMillis:     startMillis + 2100,
				TotalDurationMillis:   2000,
				CommandDurationMillis: 1500,
				HashDurationMillis:    50,
				QueueWaitMillis:       200,
			},
		},
	}
}

func TestTraceStore_WriteAndLoad(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	now := time.Now()
	trace := makeTrace("trace-001", now.UnixMilli(), "build")

	err := store.Write(ctx, trace)
	if err != nil {
		t.Fatalf("Write failed: %v", err)
	}

	// Load by exact ID and date
	date := now.UTC().Format("2006-01-02")
	loaded, err := store.Load(ctx, "trace-001", date)
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	if loaded.TraceId != "trace-001" {
		t.Errorf("expected trace ID trace-001, got %s", loaded.TraceId)
	}
	if loaded.TotalTargets != 10 {
		t.Errorf("expected 10 targets, got %d", loaded.TotalTargets)
	}
	if len(loaded.Spans) != 1 {
		t.Errorf("expected 1 span, got %d", len(loaded.Spans))
	}
	if loaded.Spans[0].CommandDurationMillis != 1500 {
		t.Errorf("expected 1500ms command duration, got %d", loaded.Spans[0].CommandDurationMillis)
	}
}

func TestTraceStore_List(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	now := time.Now()
	// Write 3 traces
	for i, id := range []string{"trace-a", "trace-b", "trace-c"} {
		trace := makeTrace(id, now.Add(time.Duration(i)*time.Minute).UnixMilli(), "build")
		if err := store.Write(ctx, trace); err != nil {
			t.Fatalf("Write %s failed: %v", id, err)
		}
	}

	entries, err := store.List(ctx)
	if err != nil {
		t.Fatalf("List failed: %v", err)
	}

	if len(entries) != 3 {
		t.Fatalf("expected 3 entries, got %d", len(entries))
	}

	// Should be sorted newest first
	if entries[0].TraceId != "trace-c" {
		t.Errorf("expected newest first (trace-c), got %s", entries[0].TraceId)
	}
}

func TestTraceStore_FindAndLoad(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	now := time.Now()
	trace := makeTrace("abcdef12-3456-7890-abcd-ef1234567890", now.UnixMilli(), "test")
	if err := store.Write(ctx, trace); err != nil {
		t.Fatalf("Write failed: %v", err)
	}

	// Find by prefix
	loaded, err := store.FindAndLoad(ctx, "abcdef12")
	if err != nil {
		t.Fatalf("FindAndLoad failed: %v", err)
	}
	if loaded.Command != "test" {
		t.Errorf("expected command 'test', got %s", loaded.Command)
	}

	// Nonexistent prefix
	_, err = store.FindAndLoad(ctx, "zzzzz")
	if err == nil {
		t.Error("expected error for nonexistent prefix")
	}
}

func TestTraceStore_Prune(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	now := time.Now()
	old := now.Add(-48 * time.Hour)
	recent := now.Add(-1 * time.Hour)

	if err := store.Write(ctx, makeTrace("old-trace", old.UnixMilli(), "build")); err != nil {
		t.Fatalf("Write old trace failed: %v", err)
	}
	if err := store.Write(ctx, makeTrace("new-trace", recent.UnixMilli(), "build")); err != nil {
		t.Fatalf("Write new trace failed: %v", err)
	}

	// Prune traces older than 24 hours
	pruned, err := store.Prune(ctx, now.Add(-24*time.Hour))
	if err != nil {
		t.Fatalf("Prune failed: %v", err)
	}
	if pruned != 1 {
		t.Errorf("expected 1 pruned, got %d", pruned)
	}

	// Verify only new trace remains
	entries, err := store.List(ctx)
	if err != nil {
		t.Fatalf("List failed: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry after prune, got %d", len(entries))
	}
	if entries[0].TraceId != "new-trace" {
		t.Errorf("expected new-trace, got %s", entries[0].TraceId)
	}
}
