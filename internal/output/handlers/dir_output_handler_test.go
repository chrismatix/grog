package handlers_test

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"io"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"grog/internal/caching"
	"grog/internal/caching/backends"
	"grog/internal/config"
	"grog/internal/label"
	"grog/internal/model"
	"grog/internal/output/handlers"
)

type recordingCacheBackend struct {
	mu    sync.Mutex
	files map[string]map[string][]byte
}

func newRecordingCacheBackend() *recordingCacheBackend {
	return &recordingCacheBackend{files: make(map[string]map[string][]byte)}
}

func (r *recordingCacheBackend) TypeName() string {
	return "recording-cache"
}

func (r *recordingCacheBackend) Get(ctx context.Context, path, key string) (io.ReadCloser, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if filesForPath, ok := r.files[path]; ok {
		if content, ok := filesForPath[key]; ok {
			return io.NopCloser(bytes.NewReader(content)), nil
		}
	}
	return nil, os.ErrNotExist
}

func (r *recordingCacheBackend) Set(ctx context.Context, path, key string, content io.Reader) error {
	buf, err := io.ReadAll(content)
	if err != nil {
		return err
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	if _, ok := r.files[path]; !ok {
		r.files[path] = make(map[string][]byte)
	}
	r.files[path][key] = buf
	return nil
}

func (r *recordingCacheBackend) Delete(ctx context.Context, path string, key string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if filesForPath, ok := r.files[path]; ok {
		delete(filesForPath, key)
		if len(filesForPath) == 0 {
			delete(r.files, path)
		}
	}
	return nil
}

func (r *recordingCacheBackend) Exists(ctx context.Context, path string, key string) (bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if filesForPath, ok := r.files[path]; ok {
		_, exists := filesForPath[key]
		return exists, nil
	}
	return false, nil
}

func (r *recordingCacheBackend) getRecording(path, key string) ([]byte, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()

	filesForPath, ok := r.files[path]
	if !ok {
		return nil, false
	}
	content, ok := filesForPath[key]
	if !ok {
		return nil, false
	}
	return content, true
}

var _ backends.CacheBackend = (*recordingCacheBackend)(nil)

func TestDirectoryOutputHandler_WriteProducesConsistentTar(t *testing.T) {
	ctx := context.Background()

	rootDir := t.TempDir()
	config.Global = config.WorkspaceConfig{Root: rootDir, WorkspaceRoot: rootDir}

	cacheBackend := newRecordingCacheBackend()
	targetCache := caching.NewTargetCache(cacheBackend)
	handler := handlers.NewDirectoryOutputHandler(targetCache)

	target := model.Target{Label: label.TL("pkg", "target"), ChangeHash: "hash"}
	output := model.NewOutput("dir", "out")

	dirPath := filepath.Join(rootDir, "pkg", "out")
	if err := os.MkdirAll(dirPath, 0o755); err != nil {
		t.Fatalf("failed to create directory: %v", err)
	}

	content := []byte("hello world")
	if err := os.WriteFile(filepath.Join(dirPath, "file.txt"), content, 0o644); err != nil {
		t.Fatalf("failed to write file: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dirPath, "empty.txt"), []byte{}, 0o644); err != nil {
		t.Fatalf("failed to write empty file: %v", err)
	}

	if err := handler.Write(ctx, target, output); err != nil {
		t.Fatalf("Write failed: %v", err)
	}

	path := targetCache.CachePath(target)
	key := targetCache.CacheKey(output)
	recorded, ok := cacheBackend.getRecording(path, key)
	if !ok {
		t.Fatalf("expected recording for %s/%s", path, key)
	}

	gzReader, err := gzip.NewReader(bytes.NewReader(recorded))
	if err != nil {
		t.Fatalf("failed to create gzip reader: %v", err)
	}
	defer gzReader.Close()

	tarReader := tar.NewReader(gzReader)
	for {
		header, err := tarReader.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("failed to read tar header: %v", err)
		}
		if header.Typeflag == tar.TypeDir {
			continue
		}

		if _, err := io.CopyN(io.Discard, tarReader, header.Size); err != nil {
			t.Fatalf("failed to read payload for %s: %v", header.Name, err)
		}
	}
}

func TestDirectoryOutputHandler_WriteAndLoad(t *testing.T) {
	ctx := context.Background()

	rootDir := t.TempDir()
	config.Global = config.WorkspaceConfig{Root: rootDir, WorkspaceRoot: rootDir}

	cacheBackend := newRecordingCacheBackend()
	targetCache := caching.NewTargetCache(cacheBackend)
	handler := handlers.NewDirectoryOutputHandler(targetCache)

	target := model.Target{Label: label.TL("pkg", "target"), ChangeHash: "hash"}
	output := model.NewOutput("dir", "out")

	dirPath := filepath.Join(rootDir, "pkg", "out")
	if err := os.MkdirAll(dirPath, 0o755); err != nil {
		t.Fatalf("failed to create directory: %v", err)
	}
	content := bytes.Repeat([]byte("hello world"), 1024*100)
	if err := os.WriteFile(filepath.Join(dirPath, "file.txt"), content, 0o644); err != nil {
		t.Fatalf("failed to write file: %v", err)
	}

	if err := handler.Write(ctx, target, output); err != nil {
		t.Fatalf("Write failed: %v", err)
	}

	if err := os.RemoveAll(dirPath); err != nil {
		t.Fatalf("failed to remove directory: %v", err)
	}

	if err := handler.Load(ctx, target, output); err != nil {
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
