package handlers

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"grog/internal/caching"
	"grog/internal/caching/backends"
	"grog/internal/config"
	"grog/internal/label"
	"grog/internal/model"
)

func TestDirectoryOutputHandler_RoundTrip_WithSymlinks(t *testing.T) {
	prev := config.Global
	tmp := t.TempDir()
	cas := t.TempDir()
	config.Global = config.WorkspaceConfig{WorkspaceRoot: tmp, Root: tmp}
	t.Cleanup(func() { config.Global = prev })

	pkg := filepath.Join(tmp, "pkg")
	outDir := filepath.Join(pkg, "out")
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(outDir, "real.txt"), []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("real.txt", filepath.Join(outDir, "link")); err != nil {
		t.Fatal(err)
	}

	fs := backends.NewFileSystemCacheForTest(tmp, cas)
	c := caching.NewCas(fs)
	h := NewDirectoryOutputHandler(c)

	tgt := model.Target{Label: label.TL("pkg", "t")}
	out := model.NewOutput("dir", "out")
	ctx := context.Background()

	prepared, err := h.Write(ctx, tgt, out, nil)
	if err != nil {
		t.Fatalf("Write: %v", err)
	}
	if err := prepared.WritePlan.Execute(ctx, nil); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	if err := os.RemoveAll(outDir); err != nil {
		t.Fatal(err)
	}

	if err := h.Load(ctx, tgt, prepared.Output, nil); err != nil {
		t.Fatalf("Load: %v", err)
	}

	if linkTarget, err := os.Readlink(filepath.Join(outDir, "link")); err != nil {
		t.Fatalf("Readlink: %v", err)
	} else if linkTarget != "real.txt" {
		t.Fatalf("got symlink target %q", linkTarget)
	}
}

func TestDirectoryOutputHandler_Write_MissingDir(t *testing.T) {
	prev := config.Global
	tmp := t.TempDir()
	cas := t.TempDir()
	config.Global = config.WorkspaceConfig{WorkspaceRoot: tmp, Root: tmp}
	t.Cleanup(func() { config.Global = prev })

	fs := backends.NewFileSystemCacheForTest(tmp, cas)
	c := caching.NewCas(fs)
	h := NewDirectoryOutputHandler(c)

	tgt := model.Target{Label: label.TL("pkg", "t")}
	out := model.NewOutput("dir", "missing-dir")
	if _, err := h.Write(context.Background(), tgt, out, nil); err == nil {
		t.Fatal("expected err for missing dir")
	}
}
