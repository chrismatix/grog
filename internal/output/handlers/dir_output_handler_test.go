package handlers_test

import (
	"bytes"
	"context"
	"errors"
	"grog/internal/caching"
	"grog/internal/caching/backends"
	"grog/internal/config"
	"grog/internal/label"
	"grog/internal/model"
	"grog/internal/output/handlers"
	"grog/internal/proto/gen"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// mockCacheBackend is a simple mock for CacheBackend to simulate failures.
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
func (m *mockCacheBackend) Size(ctx context.Context, path string, key string) (int64, error) {
	return 0, nil
}
func (m *mockCacheBackend) BeginWrite(ctx context.Context) (backends.StagedWriter, error) {
	return nil, errors.New("BeginWrite not implemented in mock")
}
func (m *mockCacheBackend) ListKeys(ctx context.Context, path string, suffix string) ([]string, error) {
	return nil, nil
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

	dirOutput, err := handler.Write(ctx, target, output, nil)
	if err != nil {
		t.Fatalf("Write failed: %v", err)
	}

	if err := handler.Load(ctx, target, dirOutput.Output, nil); err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	if got := dirOutput.Output.GetDirectory().GetTreeDigest().GetSizeBytes(); got <= 0 {
		t.Fatalf("expected tree digest sizeBytes to be populated, got %d", got)
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

	result, err := handler.Write(ctx, target, output, nil)
	if err != nil {
		// Write may still fail during hash computation (not cache write)
		return
	}
	if result.WritePlan == nil {
		t.Fatal("expected WritePlan to be non-nil")
	}
	if err := result.WritePlan.Execute(ctx, nil); err == nil {
		t.Fatal("expected WritePlan.Upload to fail when cache Set fails, got nil error")
	}
	if err := result.WritePlan.Cleanup(ctx); err != nil {
		t.Fatalf("cleanup failed: %v", err)
	}
}

// treeOnlyBackend serves a single tree digest from an inner backend but fails
// every other Get, simulating file downloads erroring out during a restore.
type treeOnlyBackend struct {
	backends.CacheBackend
	treeHash string
}

func (b *treeOnlyBackend) Get(ctx context.Context, path, key string) (io.ReadCloser, error) {
	if key == b.treeHash {
		return b.CacheBackend.Get(ctx, path, key)
	}
	return nil, errors.New("simulated download failure")
}

// TestDirectoryOutputHandler_Load_FileDownloadError_NoDeadlock reproduces issue
// #169: a flat directory (no subdirectories) whose file downloads fail must
// return an error rather than deadlocking forever in Load.
func TestDirectoryOutputHandler_Load_FileDownloadError_NoDeadlock(t *testing.T) {
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
	for i := 0; i < 64; i++ {
		name := filepath.Join(dirPath, "file"+string(rune('a'+i%26))+string(rune('0'+i/26)))
		if err := os.WriteFile(name, []byte("content"+string(rune('0'+i%10))), 0644); err != nil {
			t.Fatalf("failed to write file: %v", err)
		}
	}

	dirOutput, err := handler.Write(ctx, target, output, nil)
	if err != nil {
		t.Fatalf("Write failed: %v", err)
	}
	if err := dirOutput.WritePlan.Execute(ctx, nil); err != nil {
		t.Fatalf("WritePlan.Execute failed: %v", err)
	}

	// Remove the local directory so Load's dedup check misses and it proceeds
	// to download files from the cache.
	if err := os.RemoveAll(dirPath); err != nil {
		t.Fatalf("failed to remove local directory: %v", err)
	}

	failingCas := caching.NewCas(&treeOnlyBackend{
		CacheBackend: cacheBackend,
		treeHash:     dirOutput.Output.GetDirectory().GetTreeDigest().Hash,
	})
	failingHandler := handlers.NewDirectoryOutputHandler(failingCas)

	done := make(chan error, 1)
	go func() {
		done <- failingHandler.Load(ctx, target, dirOutput.Output, nil)
	}()

	select {
	case err := <-done:
		if err == nil {
			t.Fatal("expected Load to fail when file downloads error, got nil")
		}
	case <-time.After(30 * time.Second):
		t.Fatal("Load deadlocked: did not return within 30s")
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
			Path:       "out",
			TreeDigest: &gen.Digest{Hash: "deadbeef", SizeBytes: 0},
		}},
	}

	if err := handler.Load(ctx, target, dirOut, nil); err == nil {
		t.Fatal("expected Load to fail when cache Get fails, got nil error")
	}
}
