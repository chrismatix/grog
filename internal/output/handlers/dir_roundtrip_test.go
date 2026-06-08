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

func TestDirectoryOutputHandler_WriteAndLoadRoundTrip(t *testing.T) {
	prev := config.Global
	tmp := t.TempDir()
	cas := t.TempDir()
	config.Global = config.WorkspaceConfig{WorkspaceRoot: tmp, Root: tmp}
	t.Cleanup(func() { config.Global = prev })

	pkg := filepath.Join(tmp, "pkg")
	outDir := filepath.Join(pkg, "out")
	subDir := filepath.Join(outDir, "sub")
	if err := os.MkdirAll(subDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(outDir, "a.txt"), []byte("aaa"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(subDir, "b.txt"), []byte("bbb"), 0o755); err != nil {
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
	if prepared == nil || prepared.WritePlan == nil {
		t.Fatal("missing plan")
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

	data, err := os.ReadFile(filepath.Join(outDir, "a.txt"))
	if err != nil || string(data) != "aaa" {
		t.Fatalf("a.txt content: %v %q", err, data)
	}
	data, err = os.ReadFile(filepath.Join(subDir, "b.txt"))
	if err != nil || string(data) != "bbb" {
		t.Fatalf("sub/b.txt: %v %q", err, data)
	}
}

func TestDirectoryOutputHandler_LoadSkipsWhenHashMatches(t *testing.T) {
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
	if err := os.WriteFile(filepath.Join(outDir, "a.txt"), []byte("aaa"), 0o644); err != nil {
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
		t.Fatal(err)
	}
	if err := prepared.WritePlan.Execute(ctx, nil); err != nil {
		t.Fatal(err)
	}

	// Load without removing — should detect existing dir matches and skip.
	if err := h.Load(ctx, tgt, prepared.Output, nil); err != nil {
		t.Fatal(err)
	}
}
