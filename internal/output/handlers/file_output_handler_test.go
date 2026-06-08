package handlers_test

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"grog/internal/caching"
	"grog/internal/caching/backends"
	"grog/internal/config"
	"grog/internal/label"
	"grog/internal/model"
	"grog/internal/output/handlers"
	"grog/internal/proto/gen"
)

func setupWorkspace(t *testing.T) string {
	t.Helper()
	rootDir := t.TempDir()
	config.Global = config.WorkspaceConfig{Root: rootDir, WorkspaceRoot: rootDir}
	return rootDir
}

func newFsCas(t *testing.T) *caching.Cas {
	t.Helper()
	backend, err := backends.NewFileSystemCache(context.Background())
	if err != nil {
		t.Fatalf("failed to create cache backend: %v", err)
	}
	return caching.NewCas(backend)
}

func TestFileOutputHandler_Type(t *testing.T) {
	h := handlers.NewFileOutputHandler(nil)
	if h.Type() != handlers.HandlerType("file") {
		t.Fatalf("expected type 'file', got %q", h.Type())
	}
}

func TestFileOutputHandler_HashAndWriteAndLoad(t *testing.T) {
	ctx := context.Background()
	root := setupWorkspace(t)
	cas := newFsCas(t)
	h := handlers.NewFileOutputHandler(cas)

	pkg := "pkg"
	if err := os.MkdirAll(filepath.Join(root, pkg), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	contents := []byte("hello world")
	if err := os.WriteFile(filepath.Join(root, pkg, "out.txt"), contents, 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	target := model.Target{Label: label.TL(pkg, "t"), ChangeHash: "h"}
	output := model.NewOutput("file", "out.txt")

	gotHash, err := h.Hash(ctx, target, output)
	if err != nil {
		t.Fatalf("hash failed: %v", err)
	}
	if gotHash == "" {
		t.Fatal("expected non-empty hash")
	}

	prepared, err := h.Write(ctx, target, output, nil)
	if err != nil {
		t.Fatalf("write failed: %v", err)
	}
	if prepared.Output.GetFile().GetDigest().GetHash() != gotHash {
		t.Fatalf("digest mismatch: %q vs %q", prepared.Output.GetFile().GetDigest().GetHash(), gotHash)
	}
	if prepared.WritePlan == nil {
		t.Fatal("expected write plan")
	}

	if err := prepared.WritePlan.Execute(ctx, nil); err != nil {
		t.Fatalf("execute failed: %v", err)
	}
	if err := prepared.WritePlan.Cleanup(ctx); err != nil {
		t.Fatalf("cleanup failed: %v", err)
	}

	if err := os.Remove(filepath.Join(root, pkg, "out.txt")); err != nil {
		t.Fatalf("rm: %v", err)
	}

	if err := h.Load(ctx, target, prepared.Output, nil); err != nil {
		t.Fatalf("load failed: %v", err)
	}

	loaded, err := os.ReadFile(filepath.Join(root, pkg, "out.txt"))
	if err != nil {
		t.Fatalf("read after load: %v", err)
	}
	if !bytes.Equal(loaded, contents) {
		t.Fatalf("loaded contents mismatch")
	}
}

func TestFileOutputHandler_Load_SkipsWhenLocalMatches(t *testing.T) {
	ctx := context.Background()
	root := setupWorkspace(t)
	cas := newFsCas(t)
	h := handlers.NewFileOutputHandler(cas)

	pkg := "pkg"
	if err := os.MkdirAll(filepath.Join(root, pkg), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	contents := []byte("data")
	if err := os.WriteFile(filepath.Join(root, pkg, "out.txt"), contents, 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	target := model.Target{Label: label.TL(pkg, "t")}
	output := model.NewOutput("file", "out.txt")

	prepared, err := h.Write(ctx, target, output, nil)
	if err != nil {
		t.Fatalf("write failed: %v", err)
	}

	if err := h.Load(ctx, target, prepared.Output, nil); err != nil {
		t.Fatalf("load failed: %v", err)
	}
}

func TestFileOutputHandler_Hash_MissingFile(t *testing.T) {
	ctx := context.Background()
	setupWorkspace(t)
	h := handlers.NewFileOutputHandler(nil)
	target := model.Target{Label: label.TL("pkg", "t")}
	output := model.NewOutput("file", "missing.txt")
	if _, err := h.Hash(ctx, target, output); err == nil {
		t.Fatal("expected hash to fail on missing file")
	}
}

func TestFileOutputHandler_Write_MissingFile(t *testing.T) {
	ctx := context.Background()
	setupWorkspace(t)
	h := handlers.NewFileOutputHandler(nil)
	target := model.Target{Label: label.TL("pkg", "t")}
	output := model.NewOutput("file", "missing.txt")
	if _, err := h.Write(ctx, target, output, nil); err == nil {
		t.Fatal("expected write to fail on missing file")
	}
}

func TestFileOutputHandler_Write_FailsOnCacheSet(t *testing.T) {
	ctx := context.Background()
	root := setupWorkspace(t)

	pkg := "pkg"
	if err := os.MkdirAll(filepath.Join(root, pkg), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, pkg, "out.txt"), []byte("data"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	failing := &mockCacheBackend{setErr: errors.New("set failed")}
	cas := caching.NewCas(failing)
	h := handlers.NewFileOutputHandler(cas)

	target := model.Target{Label: label.TL(pkg, "t")}
	output := model.NewOutput("file", "out.txt")
	prepared, err := h.Write(ctx, target, output, nil)
	if err != nil {
		t.Fatalf("unexpected write error: %v", err)
	}
	if err := prepared.WritePlan.Execute(ctx, nil); err == nil {
		t.Fatal("expected execute to fail when cache set fails")
	}
	if err := prepared.WritePlan.Cleanup(ctx); err != nil {
		t.Fatalf("cleanup: %v", err)
	}
}

func TestFileOutputHandler_Load_FailsWhenCasMissing(t *testing.T) {
	ctx := context.Background()
	setupWorkspace(t)

	failing := &mockCacheBackend{getErr: errors.New("get failed")}
	cas := caching.NewCas(failing)
	h := handlers.NewFileOutputHandler(cas)
	target := model.Target{Label: label.TL("pkg", "t")}

	out := &gen.Output{Kind: &gen.Output_File{File: &gen.FileOutput{
		Path:   "out.txt",
		Digest: &gen.Digest{Hash: "deadbeef", SizeBytes: 4},
	}}}
	if err := h.Load(ctx, target, out, nil); err == nil {
		t.Fatal("expected load to fail when cache get fails")
	}
}

func TestFileWritePlan_Execute_MissingStagedFile(t *testing.T) {
	ctx := context.Background()
	root := setupWorkspace(t)
	cas := newFsCas(t)
	h := handlers.NewFileOutputHandler(cas)

	pkg := "pkg"
	if err := os.MkdirAll(filepath.Join(root, pkg), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, pkg, "out.txt"), []byte("d"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	target := model.Target{Label: label.TL(pkg, "t")}
	output := model.NewOutput("file", "out.txt")
	prepared, err := h.Write(ctx, target, output, nil)
	if err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := prepared.WritePlan.Cleanup(ctx); err != nil {
		t.Fatalf("cleanup: %v", err)
	}
	if err := prepared.WritePlan.Execute(ctx, nil); err == nil {
		t.Fatal("expected execute to fail when staged file is gone")
	}
}
