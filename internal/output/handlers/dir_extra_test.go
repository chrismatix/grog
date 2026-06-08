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

func TestDirectoryOutputHandler_TypeAndHash(t *testing.T) {
	prev := config.Global
	tmp := t.TempDir()
	cas := t.TempDir()
	config.Global = config.WorkspaceConfig{WorkspaceRoot: tmp, Root: tmp}
	t.Cleanup(func() { config.Global = prev })

	pkg := filepath.Join(tmp, "pkg")
	if err := os.MkdirAll(pkg, 0o755); err != nil {
		t.Fatal(err)
	}
	outDir := filepath.Join(pkg, "out")
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(outDir, "file.txt"), []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}

	fs := backends.NewFileSystemCacheForTest(tmp, cas)
	c := caching.NewCas(fs)
	h := NewDirectoryOutputHandler(c)
	if h.Type() != DirHandler {
		t.Fatal("type")
	}

	tgt := model.Target{Label: label.TL("pkg", "t")}
	out := model.NewOutput("dir", "out")
	hash, err := h.Hash(context.Background(), tgt, out)
	if err != nil {
		t.Fatalf("Hash: %v", err)
	}
	if hash == "" {
		t.Fatal("empty hash")
	}
}
