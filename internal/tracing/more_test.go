package tracing

import (
	"context"
	"testing"
	"time"
)

func TestTraceStore_LoadBuild_Ambiguous(t *testing.T) {
	store, _ := newTestStore(t)
	defer store.Close()
	ctx := context.Background()

	if err := store.Write(ctx, makeTestTrace("trace-amb-a", time.Now().UnixMilli(), "build")); err != nil {
		t.Fatal(err)
	}
	if err := store.Write(ctx, makeTestTrace("trace-amb-b", time.Now().Add(time.Minute).UnixMilli(), "build")); err != nil {
		t.Fatal(err)
	}

	if _, err := store.LoadBuild(ctx, "trace-amb-"); err == nil {
		t.Fatal("expected ambiguous err")
	}
}

func TestTraceStore_FindAndLoad_NotFound(t *testing.T) {
	store, _ := newTestStore(t)
	defer store.Close()
	if _, err := store.FindAndLoad(context.Background(), "nope-nope"); err == nil {
		t.Fatal("expected err")
	}
}

func TestTraceStore_Stats_FilteredCI(t *testing.T) {
	store, _ := newTestStore(t)
	defer store.Close()
	ctx := context.Background()

	if err := store.Write(ctx, makeTestTraceWithCI("c1", time.Now().UnixMilli(), "build", true)); err != nil {
		t.Fatal(err)
	}
	if err := store.Write(ctx, makeTestTraceWithCI("c2", time.Now().Add(time.Minute).UnixMilli(), "build", false)); err != nil {
		t.Fatal(err)
	}

	isCI := true
	stats, err := store.Stats(ctx, StatsOptions{Limit: 10, IsCI: &isCI})
	if err != nil {
		t.Fatalf("Stats: %v", err)
	}
	if stats.TraceCount != 1 {
		t.Fatalf("got %d", stats.TraceCount)
	}

	statsAll, err := store.Stats(ctx, StatsOptions{Limit: 10, Command: "build"})
	if err != nil {
		t.Fatalf("Stats all: %v", err)
	}
	if statsAll.TraceCount != 2 {
		t.Fatalf("got %d", statsAll.TraceCount)
	}
}

func TestTraceStore_Prune_KeepsRecent(t *testing.T) {
	store, _ := newTestStore(t)
	defer store.Close()
	ctx := context.Background()

	old := time.Now().Add(-48 * time.Hour)
	if err := store.Write(ctx, makeTestTrace("old-1", old.UnixMilli(), "build")); err != nil {
		t.Fatal(err)
	}
	recent := time.Now()
	if err := store.Write(ctx, makeTestTrace("new-1", recent.UnixMilli(), "build")); err != nil {
		t.Fatal(err)
	}

	pruned, err := store.Prune(ctx, time.Now().Add(-24*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if pruned != 1 {
		t.Fatalf("pruned %d, want 1", pruned)
	}

	entries, err := store.List(ctx, ListOptions{Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].TraceID != "new-1" {
		t.Fatalf("post-prune: %+v", entries)
	}
}
