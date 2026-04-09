package backends

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"

	"grog/internal/console"
)

// RemoteWrapper is the default implementation when using a remote cache
// It implements the logic of using the local file system first and
// falling back to the remote cache if the file is not found locally
// while updating the remote cache with local changes
type RemoteWrapper struct {
	fs     *FileSystemCache
	remote CacheBackend
}

func NewRemoteWrapper(
	fs *FileSystemCache,
	remote CacheBackend,
) *RemoteWrapper {
	return &RemoteWrapper{
		fs:     fs,
		remote: remote,
	}
}

func (rw *RemoteWrapper) GetFS() *FileSystemCache {
	return rw.fs
}

func (rw *RemoteWrapper) TypeName() string {
	return rw.remote.TypeName()
}

// Get retrieves a cached file. It first tries the local file system cache.
// If the file is not found locally, it retrieves it from the remote cache
// and stores it in the local file system cache for future access.
func (rw *RemoteWrapper) Get(ctx context.Context, path, key string) (io.ReadCloser, error) {
	logger := console.GetLogger(ctx)
	logger.Tracef("Remote wrapper fetching path: %s, key: %s", path, key)
	// Try to get the file from the local file system cache
	reader, err := rw.fs.Get(ctx, path, key)
	if err == nil {
		return reader, nil
	}

	logger.Tracef("Local cache miss for path: %s, key: %s; trying remote cache", path, key)
	// File not found locally, try the remote cache
	remoteReader, err := rw.remote.Get(ctx, path, key)
	if err != nil {
		return nil, err
	}
	defer remoteReader.Close()

	// Write the remote content into the local filesystem cache
	if err := rw.fs.Set(ctx, path, key, remoteReader); err != nil {
		return nil, err
	}

	// Now return a fresh reader from the local cache
	return rw.fs.Get(ctx, path, key)
}

// Set stores a file in both the local file system cache and the remote cache concurrently.
func (rw *RemoteWrapper) Set(ctx context.Context, path, key string, content io.Reader) error {
	console.GetLogger(ctx).Tracef("Remote wrapper writing path: %s, key: %s", path, key)
	// Create pipes for the two cache destinations
	fsRead, fsWrite := io.Pipe()
	remoteRead, remoteWrite := io.Pipe()

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	errChan := make(chan error, 2)

	var wg sync.WaitGroup
	wg.Add(3) // Track all three goroutines

	// Goroutine for writing to the filesystem cache
	go func() {
		defer wg.Done()
		defer fsRead.Close()

		if err := rw.fs.Set(ctx, path, key, fsRead); err != nil {
			select {
			case errChan <- fmt.Errorf("filesystem cache error: %w", err):
			default:
			}
		}
	}()

	// Goroutine for writing to the remote cache
	go func() {
		defer wg.Done()
		defer remoteRead.Close()

		if err := rw.remote.Set(ctx, path, key, remoteRead); err != nil {
			select {
			case errChan <- fmt.Errorf("remote cache error: %w", err):
			default:
			}
		}
	}()

	// Goroutine for copying content to both destinations
	go func() {
		defer wg.Done()
		defer fsWrite.Close() // Always close write ends to signal EOF
		defer remoteWrite.Close()

		mw := io.MultiWriter(fsWrite, remoteWrite)

		_, err := io.Copy(mw, content)
		if err != nil {
			fsWrite.CloseWithError(err)
			remoteWrite.CloseWithError(err)
		} else {
			fsWrite.Close()
			remoteWrite.Close()
		}
	}()

	wg.Wait()
	close(errChan)

	// Collect all errors (if any)
	var errs []error
	for err := range errChan {
		if err != nil {
			errs = append(errs, err)
		}
	}

	// Return a combined error if we have any
	if len(errs) > 0 {
		if len(errs) == 1 {
			return errs[0]
		}

		// Otherwise, combine all errors into one
		var errMsg strings.Builder
		errMsg.WriteString("multiple cache write errors occurred:")
		for i, err := range errs {
			errMsg.WriteString(fmt.Sprintf(" (%d) %v;", i+1, err))
		}
		return errors.New(errMsg.String())
	}

	return nil
}

// Delete removes a cached file from both the local file system cache and the remote cache.
func (rw *RemoteWrapper) Delete(ctx context.Context, path string, key string) error {
	console.GetLogger(ctx).Tracef("Remote wrapper deleting path: %s, key: %s", path, key)
	// Delete the file from the local file system cache
	err := rw.fs.Delete(ctx, path, key)
	if err != nil {
		return err
	}

	// Delete the file from the remote cache
	err = rw.remote.Delete(ctx, path, key)
	return err
}

// Exists checks if a file exists in either the local file system cache or the remote cache.
func (rw *RemoteWrapper) Exists(ctx context.Context, path string, key string) (bool, error) {
	logger := console.GetLogger(ctx)
	// Check if the file exists in the local file system cache
	localExists, err := rw.fs.Exists(ctx, path, key)
	if err != nil {
		return false, err
	}
	if localExists {
		logger.Tracef("Remote wrapper local hit for path: %s, key: %s", path, key)
		return true, nil
	}

	logger.Tracef("Remote wrapper checking remote existence for path: %s, key: %s", path, key)
	// Check if the file exists in the remote cache
	return rw.remote.Exists(ctx, path, key)
}

// ListKeys delegates to the remote backend for a complete picture of all
// keys, including those written by other machines.
func (rw *RemoteWrapper) ListKeys(ctx context.Context, path string, suffix string) ([]string, error) {
	return rw.remote.ListKeys(ctx, path, suffix)
}

// Size returns the byte size of the entry. The fast path stats the local
// filesystem cache; if the file is not yet cached locally, the wrapper asks
// the remote backend (which uses a metadata-only call like S3 HeadObject).
func (rw *RemoteWrapper) Size(ctx context.Context, path, key string) (int64, error) {
	if size, err := rw.fs.Size(ctx, path, key); err == nil {
		return size, nil
	}
	return rw.remote.Size(ctx, path, key)
}

// BeginWrite fans the streaming write out to both the local filesystem
// cache and the remote backend simultaneously, using io.Pipe + io.MultiWriter
// to keep memory usage flat. Each side has its own staged writer; Commit
// promotes both, Cancel discards both.
func (rw *RemoteWrapper) BeginWrite(ctx context.Context) (StagedWriter, error) {
	fsStaged, err := rw.fs.BeginWrite(ctx)
	if err != nil {
		return nil, fmt.Errorf("fs begin write: %w", err)
	}
	remoteStaged, err := rw.remote.BeginWrite(ctx)
	if err != nil {
		_ = fsStaged.Cancel(ctx)
		return nil, fmt.Errorf("remote begin write: %w", err)
	}

	fsRead, fsWrite := io.Pipe()
	remoteRead, remoteWrite := io.Pipe()

	w := &fanoutStagedWriter{
		fsStaged:     fsStaged,
		remoteStaged: remoteStaged,
		fsWrite:      fsWrite,
		remoteWrite:  remoteWrite,
		mw:           io.MultiWriter(fsWrite, remoteWrite),
		fsErr:        make(chan error, 1),
		remoteErr:    make(chan error, 1),
	}

	// Each goroutine drains its pipe into the corresponding staged writer.
	// Both goroutines run for the lifetime of the upload session and exit
	// when the pipe is closed (either via Commit's Close or Cancel's
	// CloseWithError).
	go func() {
		_, copyErr := io.Copy(fsStaged, fsRead)
		w.fsErr <- copyErr
		_ = fsRead.Close()
	}()
	go func() {
		_, copyErr := io.Copy(remoteStaged, remoteRead)
		w.remoteErr <- copyErr
		_ = remoteRead.Close()
	}()

	return w, nil
}

// fanoutStagedWriter mirrors a single byte stream into the local fs cache and
// the remote backend in parallel. Writes are non-blocking on either side as
// long as both readers keep up.
type fanoutStagedWriter struct {
	fsStaged     StagedWriter
	remoteStaged StagedWriter

	fsWrite     *io.PipeWriter
	remoteWrite *io.PipeWriter
	mw          io.Writer

	fsErr     chan error
	remoteErr chan error

	mu       sync.Mutex
	finished bool
}

func (w *fanoutStagedWriter) Write(p []byte) (int, error) {
	return w.mw.Write(p)
}

func (w *fanoutStagedWriter) Commit(ctx context.Context, path, key string) error {
	w.mu.Lock()
	if w.finished {
		w.mu.Unlock()
		return errors.New("remote wrapper staged writer: commit after commit/cancel")
	}
	w.finished = true
	w.mu.Unlock()

	// Close pipe writers so the drain goroutines see EOF and finish.
	_ = w.fsWrite.Close()
	_ = w.remoteWrite.Close()

	fsCopyErr := <-w.fsErr
	remoteCopyErr := <-w.remoteErr

	if fsCopyErr != nil {
		_ = w.fsStaged.Cancel(ctx)
		_ = w.remoteStaged.Cancel(ctx)
		return fmt.Errorf("fs staging copy: %w", fsCopyErr)
	}
	if remoteCopyErr != nil {
		_ = w.fsStaged.Cancel(ctx)
		_ = w.remoteStaged.Cancel(ctx)
		return fmt.Errorf("remote staging copy: %w", remoteCopyErr)
	}

	// Commit fs first because it's much cheaper to roll back (a single
	// os.Remove) than the remote side. If fs fails, we never touch remote.
	if err := w.fsStaged.Commit(ctx, path, key); err != nil {
		_ = w.remoteStaged.Cancel(ctx)
		return fmt.Errorf("fs commit: %w", err)
	}
	if err := w.remoteStaged.Commit(ctx, path, key); err != nil {
		// fs has already been promoted; the inconsistency is bounded — the
		// next read on the same machine will be served from fs and a future
		// read on a different machine will simply re-trigger the build. We
		// match RemoteWrapper.Set's existing best-effort semantics here.
		return fmt.Errorf("remote commit: %w", err)
	}
	return nil
}

func (w *fanoutStagedWriter) Cancel(ctx context.Context) error {
	w.mu.Lock()
	if w.finished {
		w.mu.Unlock()
		return nil
	}
	w.finished = true
	w.mu.Unlock()

	// Close the pipes with an error so the drain goroutines unblock and
	// abandon any in-flight writes to the staged writers.
	cancelErr := errors.New("staged write cancelled")
	_ = w.fsWrite.CloseWithError(cancelErr)
	_ = w.remoteWrite.CloseWithError(cancelErr)

	// Drain the goroutines (best effort — we don't care about the error
	// values, only that they've finished using the staged writers).
	<-w.fsErr
	<-w.remoteErr

	var firstErr error
	if err := w.fsStaged.Cancel(ctx); err != nil && firstErr == nil {
		firstErr = err
	}
	if err := w.remoteStaged.Cancel(ctx); err != nil && firstErr == nil {
		firstErr = err
	}
	return firstErr
}
