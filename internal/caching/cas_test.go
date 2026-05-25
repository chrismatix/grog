package caching

import (
	"bytes"
	"context"
	"errors"
	"io"
	"testing"

	"grog/internal/caching/backends"
	"grog/internal/caching/cachectx"
)

// stubBackend implements backends.CacheBackend just enough for these tests
// and records whether Get was ever invoked.
type stubBackend struct {
	getCalled bool
	getBytes  []byte
}

func (b *stubBackend) TypeName() string { return "stub" }
func (b *stubBackend) Get(_ context.Context, _, _ string) (io.ReadCloser, error) {
	b.getCalled = true
	if b.getBytes == nil {
		return nil, errors.New("not found")
	}
	return io.NopCloser(bytes.NewReader(b.getBytes)), nil
}
func (b *stubBackend) Set(_ context.Context, _, _ string, _ io.Reader) error { return nil }
func (b *stubBackend) Delete(_ context.Context, _, _ string) error           { return nil }
func (b *stubBackend) Exists(_ context.Context, _, _ string) (bool, error)   { return false, nil }
func (b *stubBackend) Size(_ context.Context, _, _ string) (int64, error)    { return 0, nil }
func (b *stubBackend) BeginWrite(_ context.Context) (backends.StagedWriter, error) {
	return nil, errors.New("not implemented")
}
func (b *stubBackend) ListKeys(_ context.Context, _, _ string) ([]string, error) { return nil, nil }

func TestCas_LoadHonorsSkipCASFetch(t *testing.T) {
	backend := &stubBackend{getBytes: []byte("hello")}
	cas := NewCas(backend)

	ctx := cachectx.WithSkipCASFetch(context.Background())

	if _, err := cas.Load(ctx, "abc"); !errors.Is(err, ErrCASFetchSkipped) {
		t.Fatalf("Load: expected ErrCASFetchSkipped, got %v", err)
	}
	if _, err := cas.LoadBytes(ctx, "abc"); !errors.Is(err, ErrCASFetchSkipped) {
		t.Fatalf("LoadBytes: expected ErrCASFetchSkipped, got %v", err)
	}
	if backend.getCalled {
		t.Fatal("backend.Get was called despite WithSkipCASFetch")
	}
}

func TestCas_LoadWithoutFlagHitsBackend(t *testing.T) {
	backend := &stubBackend{getBytes: []byte("hello")}
	cas := NewCas(backend)

	bytes, err := cas.LoadBytes(context.Background(), "abc")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(bytes) != "hello" {
		t.Fatalf("unexpected payload: %q", bytes)
	}
	if !backend.getCalled {
		t.Fatal("backend.Get should have been called without the skip flag")
	}
}
