package handlers_test

import (
	"bytes"
	"context"
	"grog/internal/caching"
	"grog/internal/caching/backends"
	"grog/internal/config"
	"grog/internal/label"
	"grog/internal/model"
	"grog/internal/output/handlers"
	"os"
	"path/filepath"
	"testing"
)

func TestDirectoryOutputHandler_WriteAndLoad(t *testing.T) {
	ctx := context.Background()

	rootDir := t.TempDir()
	config.Global = config.WorkspaceConfig{Root: rootDir, WorkspaceRoot: rootDir}

	cacheBackend, err := backends.NewFileSystemCache(ctx)
	if err != nil {
		t.Fatalf("failed to create cache backend: %v", err)
	}
	cas := caching.NewCas(cacheBackend)
	handler := handlers.NewDirectoryOutputHandler(cas)

	target := model.Target{Label: label.TL("pkg", "target"), ChangeHash: "hash"}
	output := model.NewOutput("dir", "out")

	dirPath := filepath.Join(rootDir, "pkg", "out")
	if err := os.MkdirAll(dirPath, 0755); err != nil {
		t.Fatalf("failed to create directory: %v", err)
	}
	content := bytes.Repeat([]byte("hello world"), 1024*100)
	if err := os.WriteFile(filepath.Join(dirPath, "file.txt"), content, 0644); err != nil {
		t.Fatalf("failed to write file: %v", err)
	}

	dirOutput, err := handler.Write(ctx, target, output)
	if _, err := handler.Write(ctx, target, output); err != nil {
		t.Fatalf("Write failed: %v", err)
	}

	if err := os.RemoveAll(dirPath); err != nil {
		t.Fatalf("failed to remove directory: %v", err)
	}

	if err := handler.Load(ctx, target, dirOutput); err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	loaded, err := os.ReadFile(filepath.Join(dirPath, "file.txt"))
	if err != nil {
		t.Fatalf("failed to read restored file: %v", err)
	}

	if !bytes.Equal(loaded, content) {
		t.Fatalf("restored file content mismatch")
	}
}
