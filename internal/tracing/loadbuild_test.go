package tracing

import (
	"context"
	"testing"
	"time"

	"grog/internal/caching/backends"
)

func newTestStore(t *testing.T) (*TraceStore, string) {
	t.Helper()
	dir := t.TempDir()
	fs := backends.NewFileSystemCacheForTest(dir, t.TempDir())
	resolver := &PathResolver{
		buildsBase: dir + "/traces/builds",
		spansBase:  dir + "/traces/spans",
	}
	store, err := NewTraceStore(fs, resolver)
	if err != nil {
		t.Fatalf("NewTraceStore: %v", err)
	}
	return store, dir
}

func TestTraceStore_LoadBuild_NotFound(t *testing.T) {
	store, _ := newTestStore(t)
	defer store.Close()
	if _, err := store.LoadBuild(context.Background(), "nope"); err == nil {
		t.Fatal("expected err for missing trace")
	}
}

func TestTraceStore_LoadBuild_NoFiles(t *testing.T) {
	dir := t.TempDir()
	fs := backends.NewFileSystemCacheForTest(dir, t.TempDir())
	resolver := &PathResolver{
		buildsBase: dir + "/empty/builds",
		spansBase:  dir + "/empty/spans",
	}
	store, err := NewTraceStore(fs, resolver)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if _, err := store.LoadBuild(context.Background(), "nope"); err == nil {
		t.Fatal("expected err")
	}
}

func TestTraceStore_LoadSpans_NoFiles(t *testing.T) {
	dir := t.TempDir()
	fs := backends.NewFileSystemCacheForTest(dir, t.TempDir())
	resolver := &PathResolver{
		buildsBase: dir + "/empty/builds",
		spansBase:  dir + "/empty/spans",
	}
	store, err := NewTraceStore(fs, resolver)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	spans, err := store.LoadSpans(context.Background(), "missing")
	if err != nil {
		t.Fatal(err)
	}
	if len(spans) != 0 {
		t.Fatalf("expected empty got %d", len(spans))
	}
}

func TestTraceStore_FindAndLoad(t *testing.T) {
	store, _ := newTestStore(t)
	defer store.Close()
	ctx := context.Background()
	if err := store.Write(ctx, makeTestTrace("trace-find-1", time.Now().UnixMilli(), "build")); err != nil {
		t.Fatal(err)
	}
	tr, err := store.FindAndLoad(ctx, "trace-find-1")
	if err != nil {
		t.Fatalf("FindAndLoad: %v", err)
	}
	if tr.Build.TraceID != "trace-find-1" {
		t.Fatalf("got %s", tr.Build.TraceID)
	}
}

func TestTraceStore_List_NoFiles(t *testing.T) {
	dir := t.TempDir()
	fs := backends.NewFileSystemCacheForTest(dir, t.TempDir())
	resolver := &PathResolver{
		buildsBase: dir + "/empty/builds",
		spansBase:  dir + "/empty/spans",
	}
	store, err := NewTraceStore(fs, resolver)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	entries, err := store.List(context.Background(), ListOptions{Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("expected empty got %d", len(entries))
	}
}

func TestTraceStore_List_FailuresOnly(t *testing.T) {
	store, _ := newTestStore(t)
	defer store.Close()
	ctx := context.Background()

	withFail := makeTestTrace("fail-1", time.Now().UnixMilli(), "build")
	withFail.Build.FailureCount = 3
	if err := store.Write(ctx, withFail); err != nil {
		t.Fatal(err)
	}
	withoutFail := makeTestTrace("ok-1", time.Now().Add(time.Minute).UnixMilli(), "build")
	withoutFail.Build.FailureCount = 0
	if err := store.Write(ctx, withoutFail); err != nil {
		t.Fatal(err)
	}

	entries, err := store.List(ctx, ListOptions{Limit: 10, FailuresOnly: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].TraceID != "fail-1" {
		t.Fatalf("got %v", entries)
	}

	since := time.Now().Add(-time.Hour)
	if _, err := store.List(ctx, ListOptions{Limit: 10, Since: &since, Command: "build"}); err != nil {
		t.Fatal(err)
	}
}

func TestTraceStore_Prune_NoFiles(t *testing.T) {
	dir := t.TempDir()
	fs := backends.NewFileSystemCacheForTest(dir, t.TempDir())
	resolver := &PathResolver{
		buildsBase: dir + "/empty/builds",
		spansBase:  dir + "/empty/spans",
	}
	store, err := NewTraceStore(fs, resolver)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	pruned, err := store.Prune(context.Background(), time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if pruned != 0 {
		t.Fatalf("pruned %d", pruned)
	}
}
