package caching

import (
	"bytes"
	"context"
	"errors"
	"io"
	"testing"

	"grog/internal/caching/backends"
	"grog/internal/config"
	"grog/internal/label"
	"grog/internal/proto/gen"
)

func newFsBackend(t *testing.T) backends.CacheBackend {
	t.Helper()
	return backends.NewFileSystemCacheForTest(t.TempDir(), t.TempDir())
}

func TestCas_RoundTripAndExistsCache(t *testing.T) {
	fs := newFsBackend(t)
	c := NewCas(fs)
	if c.GetBackend() != fs {
		t.Fatal("backend")
	}
	ctx := context.Background()

	exists, err := c.Exists(ctx, "abc")
	if err != nil || exists {
		t.Fatalf("Exists initial: %v %v", exists, err)
	}

	if err := c.WriteBytes(ctx, "abc", []byte("hi")); err != nil {
		t.Fatal(err)
	}
	if err := c.WriteBytes(ctx, "abc", []byte("hi-again")); err != nil {
		t.Fatal(err)
	}

	exists, err = c.Exists(ctx, "abc")
	if err != nil || !exists {
		t.Fatalf("after write: %v %v", exists, err)
	}

	got, err := c.LoadBytes(ctx, "abc")
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "hi" {
		t.Fatalf("got %q", got)
	}

	size, err := c.Size(ctx, "abc")
	if err != nil {
		t.Fatalf("Size: %v", err)
	}
	if size != int64(len("hi")) {
		t.Fatalf("size %d", size)
	}
}

func TestCas_LoadBytesMissing(t *testing.T) {
	c := NewCas(newFsBackend(t))
	if _, err := c.LoadBytes(context.Background(), "missing"); err == nil {
		t.Fatal("expected err")
	}
}

func TestCas_BeginWrite(t *testing.T) {
	c := NewCas(newFsBackend(t))
	sw, err := c.BeginWrite(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := io.Copy(sw, bytes.NewReader([]byte("staged"))); err != nil {
		t.Fatal(err)
	}
	if err := sw.Commit(context.Background(), "cas", "digest1"); err != nil {
		t.Fatal(err)
	}
}

type failExistsBackend struct {
	backends.CacheBackend
}

func (f *failExistsBackend) Exists(_ context.Context, _, _ string) (bool, error) {
	return false, errors.New("boom")
}

func TestCas_ExistsBackendError(t *testing.T) {
	c := NewCas(&failExistsBackend{CacheBackend: newFsBackend(t)})
	if _, err := c.Exists(context.Background(), "x"); err == nil {
		t.Fatal("expected err")
	}
}

func TestTargetResultCache_RoundTrip(t *testing.T) {
	tc := NewTargetResultCache(newFsBackend(t))
	if tc.GetBackend() == nil {
		t.Fatal("backend")
	}
	ctx := context.Background()

	target := &gen.TargetResult{ChangeHash: "ch1"}
	if err := tc.Write(ctx, target); err != nil {
		t.Fatal(err)
	}

	has, err := tc.Has(ctx, "ch1")
	if err != nil || !has {
		t.Fatalf("Has: %v %v", has, err)
	}
	hasNone, err := tc.Has(ctx, "missing")
	if err != nil || hasNone {
		t.Fatalf("Has missing: %v %v", hasNone, err)
	}

	loaded, err := tc.Load(ctx, "ch1")
	if err != nil {
		t.Fatal(err)
	}
	if loaded.ChangeHash != "ch1" {
		t.Fatalf("loaded %v", loaded)
	}

	if _, err := tc.Load(ctx, "missing"); err == nil {
		t.Fatal("expected err")
	}
}

func TestTaintStore_TaintIsTaintedClear(t *testing.T) {
	prev := config.Global
	tmp := t.TempDir()
	config.Global = config.WorkspaceConfig{Root: tmp, WorkspaceRoot: tmp}
	t.Cleanup(func() { config.Global = prev })

	ts := NewTaintStore()
	ctx := context.Background()
	l := label.TL("pkg", "t")

	tainted, err := ts.IsTainted(ctx, l)
	if err != nil || tainted {
		t.Fatal("initial")
	}

	if err := ts.Taint(ctx, l); err != nil {
		t.Fatalf("Taint: %v", err)
	}
	if err := ts.Taint(ctx, l); err != nil {
		t.Fatalf("Taint idempotent: %v", err)
	}

	tainted, err = ts.IsTainted(ctx, l)
	if err != nil || !tainted {
		t.Fatal("expected tainted")
	}

	if err := ts.Clear(ctx, l); err != nil {
		t.Fatalf("Clear: %v", err)
	}
	if err := ts.Clear(ctx, l); err != nil {
		t.Fatalf("Clear no-op: %v", err)
	}

	tainted, err = ts.IsTainted(ctx, l)
	if err != nil || tainted {
		t.Fatal("after clear")
	}
}
