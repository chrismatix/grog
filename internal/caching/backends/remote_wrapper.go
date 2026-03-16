package backends

import (
	"context"
	"errors"
	"fmt"
	"io"
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
		errMsg := "multiple cache write errors occurred:"
		for i, err := range errs {
			errMsg += fmt.Sprintf(" (%d) %v;", i+1, err)
		}
		return errors.New(errMsg)
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
