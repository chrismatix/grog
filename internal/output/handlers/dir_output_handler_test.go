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

// TestDirectoryOutputHandler_WriteAndLoad tests writing and loading directory
// outputs with the following structure:
// pkg/out/
// ├── file.txt (large file with repeated content)
// ├── link.txt (symlink to file.txt)
// └── nested/
//
//	└── nested.txt (file with different content)
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
	nestedPath := filepath.Join(dirPath, "nested")
	if err := os.MkdirAll(nestedPath, 0755); err != nil {
		t.Fatalf("failed to create directory: %v", err)
	}
	content := bytes.Repeat([]byte("hello world"), 1024*100)
	if err := os.WriteFile(filepath.Join(dirPath, "file.txt"), content, 0644); err != nil {
		t.Fatalf("failed to write file: %v", err)
	}
	nestedContent := []byte("nested content")
	if err := os.WriteFile(filepath.Join(nestedPath, "nested.txt"), nestedContent, 0644); err != nil {
		t.Fatalf("failed to write nested file: %v", err)
	}
	if err := os.Symlink("nested/nested.txt", filepath.Join(dirPath, "link.txt")); err != nil {
		t.Fatalf("failed to create symlink: %v", err)
	}
	if err := os.Symlink("../file.txt", filepath.Join(nestedPath, "uplink.txt")); err != nil {
		t.Fatalf("failed to create upward symlink: %v", err)
	}

	dirOutput, err := handler.Write(ctx, target, output)
	if _, err := handler.Write(ctx, target, output); err != nil {
		t.Fatalf("Write failed: %v", err)
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

	loadedNested, err := os.ReadFile(filepath.Join(nestedPath, "nested.txt"))
	if err != nil {
		t.Fatalf("failed to read restored nested file: %v", err)
	}
	if !bytes.Equal(loadedNested, nestedContent) {
		t.Fatalf("restored nested file content mismatch")
	}

	linkTarget, err := os.Readlink(filepath.Join(dirPath, "link.txt"))
	if err != nil {
		t.Fatalf("failed to read symlink: %v", err)
	}
	if linkTarget != "nested/nested.txt" {
		t.Fatalf("restored symlink target mismatch, got %s, want nested/nested.txt", linkTarget)
	}

	uplinkTarget, err := os.Readlink(filepath.Join(nestedPath, "uplink.txt"))
	if err != nil {
		t.Fatalf("failed to read upward symlink: %v", err)
	}
	if uplinkTarget != "../file.txt" {
		t.Fatalf("restored upward symlink target mismatch, got %s, want ../file.txt", uplinkTarget)
	}
}
