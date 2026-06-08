package tracing

import (
	"context"
	"errors"
	"testing"
	"time"

	"grog/internal/caching/backends"
	"grog/internal/config"
)

func TestIsNoFilesError(t *testing.T) {
	for _, msg := range []string{
		"No files found",
		"does not exist",
		"Cannot open file",
	} {
		if !isNoFilesError(errors.New(msg)) {
			t.Fatalf("%q should match", msg)
		}
	}
	if isNoFilesError(errors.New("other")) {
		t.Fatal("other shouldn't match")
	}
}

func TestNewPathResolver(t *testing.T) {
	prev := config.Global
	config.Global = config.WorkspaceConfig{Root: "/grog"}
	t.Cleanup(func() { config.Global = prev })

	p := NewPathResolver()
	if p == nil {
		t.Fatal("nil")
	}
	if p.BuildsGlob() == "" || p.SpansGlob() == "" {
		t.Fatal("empty glob")
	}
}

func TestTraceStore_WriteAndQuery(t *testing.T) {
	dir := t.TempDir()
	cas := t.TempDir()
	fs := backends.NewFileSystemCacheForTest(dir, cas)
	resolver := &PathResolver{
		buildsBase: dir + "/traces/builds",
		spansBase:  dir + "/traces/spans",
	}
	store, err := NewTraceStore(fs, resolver)
	if err != nil {
		t.Fatalf("NewTraceStore: %v", err)
	}
	defer store.Close()

	ctx := context.Background()
	now := time.Now()
	for i, id := range []string{"t1", "t2", "t3"} {
		trace := makeTestTraceWithCI(id, now.Add(time.Duration(i)*time.Minute).UnixMilli(), "build", i%2 == 0)
		if err := store.Write(ctx, trace); err != nil {
			t.Fatalf("Write %s: %v", id, err)
		}
	}

	got, err := store.LoadSpansForTraces(ctx, []string{"t1", "t2", "missing"})
	if err != nil {
		t.Fatalf("LoadSpansForTraces: %v", err)
	}
	if len(got["t1"]) == 0 {
		t.Fatal("expected spans for t1")
	}

	emptyResult, err := store.LoadSpansForTraces(ctx, nil)
	if err != nil || len(emptyResult) != 0 {
		t.Fatal("empty input")
	}

	rep, err := store.Bottlenecks(ctx, StatsOptions{Limit: 20})
	if err != nil {
		t.Fatalf("Bottlenecks: %v", err)
	}
	_ = rep

	repFiltered, err := store.Bottlenecks(ctx, StatsOptions{Limit: 20, Command: "build"})
	if err != nil {
		t.Fatalf("Bottlenecks filtered: %v", err)
	}
	_ = repFiltered

	isCI := true
	repCI, err := store.Bottlenecks(ctx, StatsOptions{IsCI: &isCI})
	if err != nil {
		t.Fatal(err)
	}
	_ = repCI
}

func TestTraceStore_BottlenecksEmpty(t *testing.T) {
	dir := t.TempDir()
	cas := t.TempDir()
	fs := backends.NewFileSystemCacheForTest(dir, cas)
	resolver := &PathResolver{
		buildsBase: dir + "/traces/builds",
		spansBase:  dir + "/traces/spans",
	}
	store, err := NewTraceStore(fs, resolver)
	if err != nil {
		t.Fatalf("NewTraceStore: %v", err)
	}
	defer store.Close()

	rep, err := store.Bottlenecks(context.Background(), StatsOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if rep == nil {
		t.Fatal("nil rep")
	}
}

func TestTraceStore_Pull(t *testing.T) {
	dir := t.TempDir()
	cas := t.TempDir()
	fs := backends.NewFileSystemCacheForTest(dir, cas)
	resolver := &PathResolver{
		buildsBase: dir + "/traces/builds",
		spansBase:  dir + "/traces/spans",
	}
	store, err := NewTraceStore(fs, resolver)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	ctx := context.Background()
	if err := store.Write(ctx, makeTestTrace("p1", time.Now().UnixMilli(), "build")); err != nil {
		t.Fatal(err)
	}
	progressCalls := 0
	n, err := store.Pull(ctx, func(c, total int) { progressCalls++ })
	if err != nil {
		t.Fatalf("Pull: %v", err)
	}
	_ = n
	if progressCalls == 0 {
		t.Fatal("expected progress calls")
	}

	_, err = store.Pull(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
}
