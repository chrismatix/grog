package backends

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// errReader simulates an io.Reader that always errors. Used to drive the
// Set() error path where io.Copy from content fails.
type errReader struct{}

func (errReader) Read(_ []byte) (int, error) { return 0, errors.New("read failed") }

func TestFileSystemCache_Set_ReaderError(t *testing.T) {
	fs := NewFileSystemCacheForTest(t.TempDir(), t.TempDir())
	if err := fs.Set(context.Background(), "p", "k", errReader{}); err == nil {
		t.Fatal("expected err on reader failure")
	}
}

func TestFileSystemCache_Commit_FinalDirCollision(t *testing.T) {
	// Pre-create a file where the cache wants a directory; MkdirAll(finalDir)
	// will then fail.
	ws := t.TempDir()
	cas := t.TempDir()
	fs := NewFileSystemCacheForTest(ws, cas)
	ctx := context.Background()

	sw, err := fs.BeginWrite(ctx)
	if err != nil {
		t.Fatalf("BeginWrite: %v", err)
	}
	if _, err := io.Copy(sw, bytes.NewReader([]byte("payload"))); err != nil {
		t.Fatal(err)
	}

	// Create a file at the directory path the commit would mkdir.
	clashingPath := filepath.Join(ws, "clash")
	if err := os.WriteFile(clashingPath, []byte("blocker"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Commit into a key that mkdir'd would be "clash/inside" — MkdirAll fails
	// because "clash" already exists as a regular file.
	if err := sw.Commit(ctx, "clash/inside", "k"); err == nil {
		t.Fatal("expected err on MkdirAll collision")
	}
}

func TestFileSystemCache_Commit_AfterCancel(t *testing.T) {
	fs := NewFileSystemCacheForTest(t.TempDir(), t.TempDir())
	ctx := context.Background()

	sw, err := fs.BeginWrite(ctx)
	if err != nil {
		t.Fatalf("BeginWrite: %v", err)
	}
	if err := sw.Cancel(ctx); err != nil {
		t.Fatal(err)
	}
	if err := sw.Commit(ctx, "p", "k"); err == nil {
		t.Fatal("expected commit-after-cancel err")
	}
}

func TestFileSystemCache_Exists_PermissionPath(t *testing.T) {
	fs := NewFileSystemCacheForTest(t.TempDir(), t.TempDir())
	ctx := context.Background()
	// Force a bogus path so Stat returns IsNotExist-but-not-quite. Mostly to
	// exercise both branches of Exists.
	if exists, err := fs.Exists(ctx, "nope", "key"); err != nil || exists {
		t.Fatalf("got %v %v", exists, err)
	}
}

func TestFileSystemCache_ListKeys_SuffixFilter(t *testing.T) {
	ws := t.TempDir()
	cas := t.TempDir()
	fs := NewFileSystemCacheForTest(ws, cas)
	ctx := context.Background()

	if err := fs.Set(ctx, "p", "a.parquet", strings.NewReader("1")); err != nil {
		t.Fatal(err)
	}
	if err := fs.Set(ctx, "p", "b.parquet", strings.NewReader("2")); err != nil {
		t.Fatal(err)
	}
	if err := fs.Set(ctx, "p", "c.txt", strings.NewReader("3")); err != nil {
		t.Fatal(err)
	}

	all, err := fs.ListKeys(ctx, "p", "")
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 3 {
		t.Fatalf("got %v", all)
	}

	parquet, err := fs.ListKeys(ctx, "p", ".parquet")
	if err != nil {
		t.Fatal(err)
	}
	if len(parquet) != 2 {
		t.Fatalf("got %v", parquet)
	}

	none, err := fs.ListKeys(ctx, "p", ".nope")
	if err != nil {
		t.Fatal(err)
	}
	if len(none) != 0 {
		t.Fatalf("got %v", none)
	}

	if _, err := fs.ListKeys(ctx, "missing-dir", ""); err != nil {
		t.Fatal(err)
	}
}
