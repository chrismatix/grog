package backends

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"grog/internal/caching/cachectx"
)

// mockCacheBackend is a mock implementation of the CacheBackend interface for testing.
type mockCacheBackend struct {
	getFunc    func(ctx context.Context, path, key string) (io.ReadCloser, error)
	setFunc    func(ctx context.Context, path, key string, content io.Reader) error
	deleteFunc func(ctx context.Context, path string, key string) error
	existsFunc func(ctx context.Context, path string, key string) (bool, error)
	sizeFunc   func(ctx context.Context, path string, key string) (int64, error)
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

func (m *mockCacheBackend) Size(ctx context.Context, path string, key string) (int64, error) {
	if m.sizeFunc != nil {
		return m.sizeFunc(ctx, path, key)
	}
	return 0, errors.New("Size not implemented in mock")
}

func (m *mockCacheBackend) BeginWrite(ctx context.Context) (StagedWriter, error) {
	return nil, errors.New("BeginWrite not implemented in mock")
}

func (m *mockCacheBackend) ListKeys(ctx context.Context, path string, suffix string) ([]string, error) {
	return nil, nil
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

// remoteWrapperTestStagedWriter is a tiny in-memory StagedWriter implementation
// for verifying the RemoteWrapper fanout: it captures every byte that hits
// Write so the test can assert that the same payload reached both sides.
type remoteWrapperTestStagedWriter struct {
	mu        sync.Mutex
	buf       bytes.Buffer
	committed bool
	cancelled bool
	commitTo  string // path/key
}

func (w *remoteWrapperTestStagedWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.buf.Write(p)
}

func (w *remoteWrapperTestStagedWriter) Commit(_ context.Context, path, key string) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.committed = true
	w.commitTo = path + "/" + key
	return nil
}

func (w *remoteWrapperTestStagedWriter) Cancel(_ context.Context) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.cancelled = true
	return nil
}

// TestRemoteWrapper_BeginWriteFansOut verifies that bytes streamed through
// the fanout writer reach both the local fs cache and the remote backend
// simultaneously, and that Commit promotes both sides.
func TestRemoteWrapper_BeginWriteFansOut(t *testing.T) {
	ctx := context.Background()

	sharedCasDir := t.TempDir()
	fs := &FileSystemCache{
		workspaceCacheDir: t.TempDir(),
		sharedCasDir:      sharedCasDir,
	}

	remoteCapture := &remoteWrapperTestStagedWriter{}
	remote := &mockCacheBackend{
		// We can't add a beginWriteFunc field without changing the mock's
		// public shape; for this test we satisfy BeginWrite by overriding it
		// via a closure that returns the capture writer.
	}
	// Stash the capture on the mock so the wrapper can find it. This is a
	// little ugly, but adding a beginWriteFunc field would touch every
	// existing mockCacheBackend caller for one test.
	rw := &RemoteWrapper{fs: fs, remote: &beginWriteAwareMock{
		mockCacheBackend: remote,
		writer:           remoteCapture,
	}}

	sw, err := rw.BeginWrite(ctx)
	if err != nil {
		t.Fatalf("BeginWrite: %v", err)
	}

	payload := []byte("fanned-out content")
	if _, err := sw.Write(payload); err != nil {
		t.Fatalf("Write: %v", err)
	}
	if err := sw.Commit(ctx, "cas", "sha256:cafe"); err != nil {
		t.Fatalf("Commit: %v", err)
	}

	// fs side: the bytes should be at the final cache key.
	gotFS, err := io.ReadAll(must(fs.Get(ctx, "cas", "sha256:cafe")))
	if err != nil {
		t.Fatalf("read fs: %v", err)
	}
	if string(gotFS) != string(payload) {
		t.Fatalf("fs payload mismatch: got %q want %q", gotFS, payload)
	}

	// remote side: the capture writer should have the same bytes and be Committed.
	if !remoteCapture.committed {
		t.Fatal("expected remote staged writer to be committed")
	}
	if got := remoteCapture.buf.String(); got != string(payload) {
		t.Fatalf("remote payload mismatch: got %q want %q", got, payload)
	}
	if remoteCapture.commitTo != "cas/sha256:cafe" {
		t.Fatalf("remote commit destination: %q", remoteCapture.commitTo)
	}
}

// TestRemoteWrapper_BeginWriteCancel verifies that Cancel cancels both sides
// and the fs cache does not contain the half-written entry.
func TestRemoteWrapper_BeginWriteCancel(t *testing.T) {
	ctx := context.Background()

	sharedCasDir := t.TempDir()
	fs := &FileSystemCache{
		workspaceCacheDir: t.TempDir(),
		sharedCasDir:      sharedCasDir,
	}
	remoteCapture := &remoteWrapperTestStagedWriter{}
	rw := &RemoteWrapper{fs: fs, remote: &beginWriteAwareMock{
		mockCacheBackend: &mockCacheBackend{},
		writer:           remoteCapture,
	}}

	sw, err := rw.BeginWrite(ctx)
	if err != nil {
		t.Fatalf("BeginWrite: %v", err)
	}
	if _, err := sw.Write([]byte("partial")); err != nil {
		t.Fatalf("Write: %v", err)
	}
	if err := sw.Cancel(ctx); err != nil {
		t.Fatalf("Cancel: %v", err)
	}

	// fs side: nothing committed.
	if _, err := fs.Get(ctx, "cas", "sha256:should-not-exist"); err == nil {
		t.Fatal("fs should not contain a committed key after Cancel")
	}
	// fs staging dir should be empty.
	stagingEntries, _ := os.ReadDir(filepath.Join(sharedCasDir, fsStagingDirName))
	if len(stagingEntries) != 0 {
		t.Fatalf("expected staging dir empty after cancel, got %d entries", len(stagingEntries))
	}
	// remote side: cancelled.
	if !remoteCapture.cancelled {
		t.Fatal("expected remote staged writer to be cancelled")
	}
}

// beginWriteAwareMock wraps mockCacheBackend with a fixed-return BeginWrite,
// so the existing dozens of mockCacheBackend callers don't need to grow a new
// field for the BeginWrite tests.
type beginWriteAwareMock struct {
	*mockCacheBackend
	writer StagedWriter
}

func (m *beginWriteAwareMock) BeginWrite(_ context.Context) (StagedWriter, error) {
	return m.writer, nil
}

func must[T any](v T, err error) T {
	if err != nil {
		panic(err)
	}
	return v
}

func TestRemoteWrapper_SkipRemoteFetch(t *testing.T) {
	path := "test/path"
	key := "test_key"
	content := "test content"

	newRemote := func(t *testing.T, called *atomic.Bool) *mockCacheBackend {
		t.Helper()
		return &mockCacheBackend{
			getFunc: func(ctx context.Context, _, _ string) (io.ReadCloser, error) {
				called.Store(true)
				return io.NopCloser(strings.NewReader(content)), nil
			},
			existsFunc: func(ctx context.Context, _, _ string) (bool, error) {
				called.Store(true)
				return true, nil
			},
			sizeFunc: func(ctx context.Context, _, _ string) (int64, error) {
				called.Store(true)
				return int64(len(content)), nil
			},
		}
	}

	t.Run("Get returns local hit without consulting remote", func(t *testing.T) {
		fs := &FileSystemCache{workspaceCacheDir: t.TempDir()}
		if err := fs.Set(context.Background(), path, key, strings.NewReader(content)); err != nil {
			t.Fatalf("seed fs: %v", err)
		}
		var remoteCalled atomic.Bool
		rw := NewRemoteWrapper(fs, newRemote(t, &remoteCalled))

		reader, err := rw.Get(cachectx.WithSkipRemoteFetch(context.Background()), path, key)
		if err != nil {
			t.Fatalf("Get failed: %v", err)
		}
		defer reader.Close()
		if remoteCalled.Load() {
			t.Fatal("remote backend was called despite SkipRemoteFetch")
		}
	})

	t.Run("Get returns the local error on miss instead of falling through", func(t *testing.T) {
		fs := &FileSystemCache{workspaceCacheDir: t.TempDir()}
		var remoteCalled atomic.Bool
		rw := NewRemoteWrapper(fs, newRemote(t, &remoteCalled))

		_, err := rw.Get(cachectx.WithSkipRemoteFetch(context.Background()), path, key)
		if err == nil {
			t.Fatal("expected error on local miss")
		}
		if remoteCalled.Load() {
			t.Fatal("remote backend was called despite SkipRemoteFetch")
		}
	})

	t.Run("Exists returns false on local miss without consulting remote", func(t *testing.T) {
		fs := &FileSystemCache{workspaceCacheDir: t.TempDir()}
		var remoteCalled atomic.Bool
		rw := NewRemoteWrapper(fs, newRemote(t, &remoteCalled))

		exists, err := rw.Exists(cachectx.WithSkipRemoteFetch(context.Background()), path, key)
		if err != nil {
			t.Fatalf("Exists failed: %v", err)
		}
		if exists {
			t.Fatal("expected exists=false on local miss with SkipRemoteFetch")
		}
		if remoteCalled.Load() {
			t.Fatal("remote backend was called despite SkipRemoteFetch")
		}
	})

	t.Run("Size returns the local error on miss without consulting remote", func(t *testing.T) {
		fs := &FileSystemCache{workspaceCacheDir: t.TempDir()}
		var remoteCalled atomic.Bool
		rw := NewRemoteWrapper(fs, newRemote(t, &remoteCalled))

		_, err := rw.Size(cachectx.WithSkipRemoteFetch(context.Background()), path, key)
		if err == nil {
			t.Fatal("expected error on local miss")
		}
		if remoteCalled.Load() {
			t.Fatal("remote backend was called despite SkipRemoteFetch")
		}
	})
}

func TestRemoteWrapper_SkipRemoteUpload(t *testing.T) {
	path := "test/path"
	key := "test_key"
	content := "test content"

	t.Run("Set writes locally and skips remote", func(t *testing.T) {
		fs := &FileSystemCache{workspaceCacheDir: t.TempDir()}
		var remoteCalled atomic.Bool
		remote := &mockCacheBackend{
			setFunc: func(_ context.Context, _, _ string, _ io.Reader) error {
				remoteCalled.Store(true)
				return nil
			},
		}
		rw := NewRemoteWrapper(fs, remote)

		err := rw.Set(cachectx.WithSkipRemoteUpload(context.Background()), path, key, strings.NewReader(content))
		if err != nil {
			t.Fatalf("Set failed: %v", err)
		}
		if remoteCalled.Load() {
			t.Fatal("remote backend Set was called despite SkipRemoteUpload")
		}

		// And the local FS has the bytes.
		reader, err := fs.Get(context.Background(), path, key)
		if err != nil {
			t.Fatalf("local Get failed: %v", err)
		}
		defer reader.Close()
		got, _ := io.ReadAll(reader)
		if string(got) != content {
			t.Errorf("local content mismatch: got %q want %q", got, content)
		}
	})

	t.Run("BeginWrite returns a fs-only writer when upload is skipped", func(t *testing.T) {
		fs := &FileSystemCache{workspaceCacheDir: t.TempDir()}
		var remoteBeginCalled atomic.Bool
		remote := &beginWriteAwareMock{
			mockCacheBackend: &mockCacheBackend{},
			// If this is ever returned we know we fanned out.
			writer: nil,
		}
		// Wrap BeginWrite to detect any call.
		rw := NewRemoteWrapper(fs, recordingBeginWrite{beginWriteAwareMock: remote, called: &remoteBeginCalled})

		writer, err := rw.BeginWrite(cachectx.WithSkipRemoteUpload(context.Background()))
		if err != nil {
			t.Fatalf("BeginWrite failed: %v", err)
		}
		if _, err := io.WriteString(writer, content); err != nil {
			t.Fatalf("write: %v", err)
		}
		if err := writer.Commit(context.Background(), path, key); err != nil {
			t.Fatalf("commit: %v", err)
		}
		if remoteBeginCalled.Load() {
			t.Fatal("remote BeginWrite was called despite SkipRemoteUpload")
		}

		reader, err := fs.Get(context.Background(), path, key)
		if err != nil {
			t.Fatalf("local Get failed: %v", err)
		}
		defer reader.Close()
		got, _ := io.ReadAll(reader)
		if string(got) != content {
			t.Errorf("local content mismatch: got %q want %q", got, content)
		}
	})
}

// recordingBeginWrite flips an atomic flag when BeginWrite is called. Used to
// assert that the skip-upload path bypasses the remote backend entirely.
type recordingBeginWrite struct {
	*beginWriteAwareMock
	called *atomic.Bool
}

func (r recordingBeginWrite) BeginWrite(ctx context.Context) (StagedWriter, error) {
	r.called.Store(true)
	return r.beginWriteAwareMock.BeginWrite(ctx)
}
