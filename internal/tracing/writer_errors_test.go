package tracing

import (
	"context"
	"errors"
	"io"
	"testing"

	"grog/internal/caching/backends"
)

type failingBackend struct {
	backends.CacheBackend
	failOnPath string
}

func (f *failingBackend) Set(_ context.Context, path, _ string, _ io.Reader) error {
	if f.failOnPath != "" && path[:len(f.failOnPath)] == f.failOnPath {
		return errors.New("set failed for " + path)
	}
	return nil
}

func TestTraceWriter_Write_BuildsSetFails(t *testing.T) {
	fs := backends.NewFileSystemCacheForTest(t.TempDir(), t.TempDir())
	w := NewTraceWriter(&failingBackend{CacheBackend: fs, failOnPath: tracesBuildsPath})
	err := w.Write(context.Background(), makeTestTrace("t", 1700000000000, "build"))
	if err == nil {
		t.Fatal("expected err on builds Set")
	}
}

func TestTraceWriter_Write_SpansSetFails(t *testing.T) {
	fs := backends.NewFileSystemCacheForTest(t.TempDir(), t.TempDir())
	w := NewTraceWriter(&failingBackend{CacheBackend: fs, failOnPath: tracesSpansPath})
	err := w.Write(context.Background(), makeTestTrace("t", 1700000000000, "build"))
	if err == nil {
		t.Fatal("expected err on spans Set")
	}
}

func TestTraceWriter_Write_NoSpans(t *testing.T) {
	fs := backends.NewFileSystemCacheForTest(t.TempDir(), t.TempDir())
	w := NewTraceWriter(fs)
	trace := makeTestTrace("t", 1700000000000, "build")
	trace.Spans = nil
	if err := w.Write(context.Background(), trace); err != nil {
		t.Fatalf("Write: %v", err)
	}
}
