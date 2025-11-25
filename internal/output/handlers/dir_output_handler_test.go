package handlers_test

import (
    "bytes"
    "context"
    "errors"
    "io"
    "grog/internal/caching"
    "grog/internal/caching/backends"
    "grog/internal/config"
    "grog/internal/label"
    "grog/internal/model"
    "grog/internal/output/handlers"
    "grog/internal/proto/gen"
    "os"
    "path/filepath"
    "testing"
)

// mockCacheBackend is a simple mock for CacheBackend to simulate failures
type mockCacheBackend struct {
    setErr error
    getErr error
}

func (m *mockCacheBackend) TypeName() string { return "mock" }
func (m *mockCacheBackend) Get(ctx context.Context, path, key string) (io.ReadCloser, error) {
    if m.getErr != nil {
        return nil, m.getErr
    }
    return io.NopCloser(bytes.NewReader(nil)), nil
}
func (m *mockCacheBackend) Set(ctx context.Context, path, key string, content io.Reader) error {
    if m.setErr != nil {
        return m.setErr
    }
    // drain content to simulate full read
    _, _ = io.Copy(io.Discard, content)
    return nil
}
func (m *mockCacheBackend) Delete(ctx context.Context, path string, key string) error { return nil }
func (m *mockCacheBackend) Exists(ctx context.Context, path string, key string) (bool, error) {
    return false, nil
}

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

// TestDirectoryOutputHandler_Write_FailsOnCacheWrite ensures that when the
// cache backend fails on Set (writing either files or the tree), the Write
// operation returns an error.
func TestDirectoryOutputHandler_Write_FailsOnCacheWrite(t *testing.T) {
    ctx := context.Background()

    rootDir := t.TempDir()
    config.Global = config.WorkspaceConfig{Root: rootDir, WorkspaceRoot: rootDir}

    // Prepare a minimal directory
    dirPath := filepath.Join(rootDir, "pkg", "out")
    if err := os.MkdirAll(dirPath, 0o755); err != nil {
        t.Fatalf("failed to create directory: %v", err)
    }
    if err := os.WriteFile(filepath.Join(dirPath, "file.txt"), []byte("data"), 0o644); err != nil {
        t.Fatalf("failed to write file: %v", err)
    }

    // Mock cache that fails on Set
    failing := &mockCacheBackend{setErr: errors.New("backend set failed")}
    cas := caching.NewCas(failing)
    handler := handlers.NewDirectoryOutputHandler(cas)

    target := model.Target{Label: label.TL("pkg", "target"), ChangeHash: "hash"}
    output := model.NewOutput("dir", "out")

    if _, err := handler.Write(ctx, target, output); err == nil {
        t.Fatal("expected Write to fail when cache Set fails, got nil error")
    }
}

// TestDirectoryOutputHandler_Load_FailsOnCacheLoad ensures that when the
// cache backend fails on Get (loading the tree), the Load operation returns an error.
func TestDirectoryOutputHandler_Load_FailsOnCacheLoad(t *testing.T) {
    ctx := context.Background()

    rootDir := t.TempDir()
    config.Global = config.WorkspaceConfig{Root: rootDir, WorkspaceRoot: rootDir}

    // Mock cache that fails on Get
    failing := &mockCacheBackend{getErr: errors.New("backend get failed")}
    cas := caching.NewCas(failing)
    handler := handlers.NewDirectoryOutputHandler(cas)

    target := model.Target{Label: label.TL("pkg", "target"), ChangeHash: "hash"}

    // Prepare a minimal output pointing to some digest
    dirOut := &gen.Output{
        Kind: &gen.Output_Directory{Directory: &gen.DirectoryOutput{
            Path: "out",
            TreeDigest: &gen.Digest{Hash: "deadbeef", SizeBytes: 0},
        }},
    }

    if err := handler.Load(ctx, target, dirOut); err == nil {
        t.Fatal("expected Load to fail when cache Get fails, got nil error")
    }
}
