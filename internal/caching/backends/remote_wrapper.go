package backends

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"grog/internal/console"
	"io"
	"sync"
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

func (rw *RemoteWrapper) TypeName() string {
	return rw.remote.TypeName()
}

// Get retrieves a cached file. It first tries the local file system cache.
// If the file is not found locally, it retrieves it from the remote cache
// and stores it in the local file system cache for future access.
func (rw *RemoteWrapper) Get(ctx context.Context, path, key string) (io.ReadCloser, error) {
	// Try to get the file from the local file system cache
	reader, err := rw.fs.Get(ctx, path, key)
	if err == nil {
		// File found locally, return it
		return reader, nil
	}

	// File not found locally, try the remote cache
	reader, err = rw.remote.Get(ctx, path, key)
	if err != nil {
		// File not found in remote cache, return the error
		return nil, err
	}

	// File found in remote cache, store it in the local file system cache for future access
	// Create a buffer to read the content from the remote reader and store it in both the local cache and the return reader
	var buf bytes.Buffer
	teeReader := io.TeeReader(reader, &buf)

	// Store the file in the local cache asynchronously, so it doesn't block the Get request
	go func() {
		err := rw.fs.Set(ctx, path, key, &buf)
		if err != nil {
			logger := console.GetLogger(ctx)
			logger.Errorf("Failed to store file in local cache after retrieving from remote: %v", err)
		}
		// Close the reader after storing the content locally
		reader.Close()
	}()
	return io.NopCloser(teeReader), nil
}

// Set stores a file in both the local file system cache and the remote cache concurrently.
func (rw *RemoteWrapper) Set(ctx context.Context, path, key string, content io.Reader) error {
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
	// Check if the file exists in the local file system cache
	localExists, err := rw.fs.Exists(ctx, path, key)
	if err != nil {
		return false, err
	}
	if localExists {
		return true, nil
	}

	// Check if the file exists in the remote cache
	return rw.remote.Exists(ctx, path, key)
}

// Clear removes all files from both the local file system cache and the remote cache.
func (rw *RemoteWrapper) Clear(ctx context.Context, expunge bool) error {
	// Clear the local file system cache only
	return rw.fs.Clear(ctx, expunge)
}
