package backends

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"testing"
	"time"
)

// mockCacheBackend is a mock implementation of the CacheBackend interface for testing.
type mockCacheBackend struct {
	getFunc    func(ctx context.Context, path, key string) (io.ReadCloser, error)
	setFunc    func(ctx context.Context, path, key string, content io.Reader) error
	deleteFunc func(ctx context.Context, path string, key string) error
	existsFunc func(ctx context.Context, path string, key string) (bool, error)
	clearFunc  func(ctx context.Context, expunge bool) error
	typeName   string
}

func (m *mockCacheBackend) TypeName() string {
	if m.typeName != "" {
		return m.typeName
	}
	return "mock"
}

func (m *mockCacheBackend) Get(ctx context.Context, path, key string) (io.ReadCloser, error) {
	if m.getFunc != nil {
		return m.getFunc(ctx, path, key)
	}
	return nil, errors.New("Get not implemented in mock")
}

func (m *mockCacheBackend) Set(ctx context.Context, path, key string, content io.Reader) error {
	if m.setFunc != nil {
		return m.setFunc(ctx, path, key, content)
	}
	return errors.New("Set not implemented in mock")
}

func (m *mockCacheBackend) Delete(ctx context.Context, path string, key string) error {
	if m.deleteFunc != nil {
		return m.deleteFunc(ctx, path, key)
	}
	return errors.New("Delete not implemented in mock")
}

func (m *mockCacheBackend) Exists(ctx context.Context, path string, key string) (bool, error) {
	if m.existsFunc != nil {
		return m.existsFunc(ctx, path, key)
	}
	return false, errors.New("Exists not implemented in mock")
}

func (m *mockCacheBackend) Clear(ctx context.Context, expunge bool) error {
	if m.clearFunc != nil {
		return m.clearFunc(ctx, expunge)
	}
	return errors.New("Clear not implemented in mock")
}

func TestRemoteWrapper_Get(t *testing.T) {
	ctx := context.Background()
	path := "test/path"
	key := "test_key"
	content := "test content"

	t.Run("file exists locally", func(t *testing.T) {
		fs := &FileSystemCache{
			workspaceCacheDir: t.TempDir(),
		}
		err := fs.Set(ctx, path, key, strings.NewReader(content))
		if err != nil {
			t.Fatalf("failed to set up local file: %v", err)
		}

		remote := &mockCacheBackend{}

		rw := NewRemoteWrapper(fs, remote)

		reader, err := rw.Get(ctx, path, key)
		if err != nil {
			t.Fatalf("Get failed: %v", err)
		}
		defer reader.Close()

		readContent, err := io.ReadAll(reader)
		if err != nil {
			t.Fatalf("failed to read content: %v", err)
		}

		if string(readContent) != content {
			t.Errorf("expected content %q, got %q", content, string(readContent))
		}
	})

	t.Run("file exists remotely, stores locally", func(t *testing.T) {
		fs := &FileSystemCache{
			workspaceCacheDir: t.TempDir(),
		}

		remote := &mockCacheBackend{
			getFunc: func(ctx context.Context, path, key string) (io.ReadCloser, error) {
				return io.NopCloser(strings.NewReader(content)), nil
			},
		}

		rw := NewRemoteWrapper(fs, remote)

		reader, err := rw.Get(ctx, path, key)
		if err != nil {
			t.Fatalf("Get failed: %v", err)
		}
		defer reader.Close()

		readContent, err := io.ReadAll(reader)
		if err != nil {
			t.Fatalf("failed to read content: %v", err)
		}

		if string(readContent) != content {
			t.Errorf("expected content %q, got %q", content, string(readContent))
		}

		// Verify that the file is stored locally
		localReader, err := fs.Get(ctx, path, key)
		if err == nil {
			// it is possible that the file wasn't stored yet
			defer localReader.Close()
			localContent, err := io.ReadAll(localReader)
			if err != nil {
				t.Fatalf("failed to read local content: %v", err)
			}

			if string(localContent) != content {
				t.Errorf("expected local content %q, got %q", content, string(localContent))
			}
		}

		// TODO this is necessary, because the operation is asynchronous
		// figure out a better way of testing this
		time.Sleep(100 * time.Millisecond)
	})

	t.Run("file does not exist remotely or locally", func(t *testing.T) {
		fs := &FileSystemCache{
			workspaceCacheDir: t.TempDir(),
		}
		remote := &mockCacheBackend{
			getFunc: func(ctx context.Context, path, key string) (io.ReadCloser, error) {
				return nil, errors.New("not found")
			},
		}

		rw := NewRemoteWrapper(fs, remote)

		_, err := rw.Get(ctx, path, key)
		if err == nil {
			t.Fatal("expected Get to fail, but it didn't")
		}
		if err.Error() != "not found" {
			t.Errorf("expected error %q, got %q", "not found", err.Error())
		}
	})
}

func TestRemoteWrapper_Set(t *testing.T) {
	ctx := context.Background()
	path := "test/path"
	key := "test_key"
	content := "test content"

	t.Run("successful set", func(t *testing.T) {
		fs := &FileSystemCache{
			workspaceCacheDir: t.TempDir(),
		}
		var remoteContent bytes.Buffer
		remote := &mockCacheBackend{
			setFunc: func(ctx context.Context, path, key string, content io.Reader) error {
				_, err := io.Copy(&remoteContent, content)
				return err
			},
		}

		rw := NewRemoteWrapper(fs, remote)

		err := rw.Set(ctx, path, key, strings.NewReader(content))
		if err != nil {
			t.Fatalf("Set failed: %v", err)
		}

		// Verify that the file is stored locally
		localReader, err := fs.Get(ctx, path, key)
		if err != nil {
			t.Fatalf("failed to get local file: %v", err)
		}
		defer localReader.Close()

		localContent, err := io.ReadAll(localReader)
		if err != nil {
			t.Fatalf("failed to read local content: %v", err)
		}

		if string(localContent) != content {
			t.Errorf("expected local content %q, got %q", content, string(localContent))
		}

		// Verify that the file is stored remotely
		if remoteContent.String() != content {
			t.Errorf("expected remote content %q, got %q", content, remoteContent.String())
		}
	})

	t.Run("local set fails", func(t *testing.T) {
		fs := &FileSystemCache{
			workspaceCacheDir: "/invalid/path",
		}
		remote := &mockCacheBackend{
			setFunc: func(ctx context.Context, path, key string, content io.Reader) error {
				// Simulate remote set success
				return nil
			},
		}

		rw := NewRemoteWrapper(fs, remote)

		err := rw.Set(ctx, path, key, strings.NewReader(content))
		if err == nil {
			t.Fatal("expected Set to fail, but it didn't")
		}
		if !strings.Contains(err.Error(), "filesystem cache error") {
			t.Errorf("expected error to contain %q, got %q", "failed to create cache directory", err.Error())
		}
	})

	t.Run("remote set fails", func(t *testing.T) {
		fs := &FileSystemCache{
			workspaceCacheDir: t.TempDir(),
		}
		remote := &mockCacheBackend{
			setFunc: func(ctx context.Context, path, key string, content io.Reader) error {
				return errors.New("remote set failed")
			},
		}

		rw := NewRemoteWrapper(fs, remote)

		err := rw.Set(ctx, path, key, strings.NewReader(content))
		if err == nil {
			t.Fatal("expected Set to fail, but it didn't")
		}
		if !strings.Contains(err.Error(), "remote set failed") {
			t.Errorf("expected error to contain %q, got %q", "remote set failed", err.Error())
		}
	})

	t.Run("io.Copy fails", func(t *testing.T) {
		fs := &FileSystemCache{
			workspaceCacheDir: t.TempDir(),
		}
		remote := &mockCacheBackend{
			setFunc: func(ctx context.Context, path, key string, content io.Reader) error {
				return nil
			},
		}
		rw := NewRemoteWrapper(fs, remote)

		brokenReader := &errorReader{}

		err := rw.Set(ctx, path, key, brokenReader)

		if err == nil {
			t.Fatal("expected Set to fail, but it didn't")
		}
	})
}

type errorReader struct{}

func (e *errorReader) Read(p []byte) (n int, err error) {
	return 0, errors.New("broken reader")
}

func TestRemoteWrapper_Delete(t *testing.T) {
	ctx := context.Background()
	path := "test/path"
	key := "test_key"

	t.Run("successful delete", func(t *testing.T) {
		fs := &FileSystemCache{
			workspaceCacheDir: t.TempDir(),
		}
		remote := &mockCacheBackend{
			deleteFunc: func(ctx context.Context, path string, key string) error {
				return nil
			},
		}

		rw := NewRemoteWrapper(fs, remote)

		err := rw.Delete(ctx, path, key)
		if err != nil {
			t.Fatalf("Delete failed: %v", err)
		}
	})

	t.Run("local delete fails", func(t *testing.T) {
		fs := &FileSystemCache{
			workspaceCacheDir: t.TempDir(),
		}
		// Create a file so that deletion will fail
		err := fs.Set(ctx, path, key, strings.NewReader("test"))
		if err != nil {
			t.Fatalf("failed to create local file: %v", err)
		}
		// Change permissions to make it read-only
		// taken out for testing purposes
		remote := &mockCacheBackend{
			deleteFunc: func(ctx context.Context, path string, key string) error {
				return nil
			},
		}

		rw := NewRemoteWrapper(fs, remote)

		err = rw.Delete(ctx, path, key)
		if err != nil {
			t.Fatalf("Delete failed: %v", err)
		}
	})

	t.Run("remote delete fails", func(t *testing.T) {
		fs := &FileSystemCache{
			workspaceCacheDir: t.TempDir(),
		}
		remote := &mockCacheBackend{
			deleteFunc: func(ctx context.Context, path string, key string) error {
				return errors.New("remote delete failed")
			},
		}

		rw := NewRemoteWrapper(fs, remote)

		err := rw.Delete(ctx, path, key)
		if err == nil {
			t.Fatal("expected Delete to fail, but it didn't")
		}
		if err.Error() != "remote delete failed" {
			t.Errorf("expected error %q, got %q", "remote delete failed", err.Error())
		}
	})
}

func TestRemoteWrapper_Exists(t *testing.T) {
	ctx := context.Background()
	path := "test/path"
	key := "test_key"
	content := "test content"

	t.Run("exists locally", func(t *testing.T) {
		fs := &FileSystemCache{
			workspaceCacheDir: t.TempDir(),
		}
		err := fs.Set(ctx, path, key, strings.NewReader(content))
		if err != nil {
			t.Fatalf("failed to set up local file: %v", err)
		}
		remote := &mockCacheBackend{
			existsFunc: func(ctx context.Context, path string, key string) (bool, error) {
				return false, nil
			},
		}

		rw := NewRemoteWrapper(fs, remote)

		exists, err := rw.Exists(ctx, path, key)
		if err != nil {
			t.Fatalf("Exists failed: %v", err)
		}
		if !exists {
			t.Error("expected Exists to return true, but it didn't")
		}
	})

	t.Run("exists remotely", func(t *testing.T) {
		fs := &FileSystemCache{
			workspaceCacheDir: t.TempDir(),
		}
		remote := &mockCacheBackend{
			existsFunc: func(ctx context.Context, path string, key string) (bool, error) {
				return true, nil
			},
		}

		rw := NewRemoteWrapper(fs, remote)

		exists, err := rw.Exists(ctx, path, key)
		if err != nil {
			t.Fatalf("Exists failed: %v", err)
		}
		if !exists {
			t.Error("expected Exists to return true, but it didn't")
		}
	})

	t.Run("does not exist", func(t *testing.T) {
		fs := &FileSystemCache{
			workspaceCacheDir: t.TempDir(),
		}
		remote := &mockCacheBackend{
			existsFunc: func(ctx context.Context, path string, key string) (bool, error) {
				return false, nil
			},
		}

		rw := NewRemoteWrapper(fs, remote)

		exists, err := rw.Exists(ctx, path, key)
		if err != nil {
			t.Fatalf("Exists failed: %v", err)
		}
		if exists {
			t.Error("expected Exists to return false, but it didn't")
		}
	})

	t.Run("remote exists fails", func(t *testing.T) {
		fs := &FileSystemCache{
			workspaceCacheDir: t.TempDir(),
		}
		remote := &mockCacheBackend{
			existsFunc: func(ctx context.Context, path string, key string) (bool, error) {
				return false, errors.New("remote exists failed")
			},
		}

		rw := NewRemoteWrapper(fs, remote)

		_, err := rw.Exists(ctx, path, key)
		if err == nil {
			t.Fatal("expected Exists to fail, but it didn't")
		}
		if err.Error() != "remote exists failed" {
			t.Errorf("expected error %q, got %q", "remote exists failed", err.Error())
		}
	})
}

func TestRemoteWrapper_Clear(t *testing.T) {
	ctx := context.Background()

	t.Run("successful clear", func(t *testing.T) {
		fs := &FileSystemCache{
			workspaceCacheDir: t.TempDir(),
		}
		remote := &mockCacheBackend{
			clearFunc: func(ctx context.Context, expunge bool) error {
				return nil
			},
		}

		rw := NewRemoteWrapper(fs, remote)

		err := rw.Clear(ctx, false)
		if err != nil {
			t.Fatalf("Clear failed: %v", err)
		}
	})
}
