package backends

import (
	"bytes"
	"context"
	"io"
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
			rw.fs.logger.Errorf("Failed to store file in local cache after retrieving from remote: %v", err)
		}
		// Close the reader after storing the content locally
		reader.Close()
	}()
	return io.NopCloser(teeReader), nil
}

// Set stores a file in both the local file system cache and the remote cache.
func (rw *RemoteWrapper) Set(ctx context.Context, path, key string, content io.Reader) error {
	// Store the file in the local file system cache
	err := rw.fs.Set(ctx, path, key, content)
	if err != nil {
		return err
	}

	// Reset the reader to the beginning so that it can be read again
	// Wrap the content reader with a buffer to allow reading multiple times

	var buf bytes.Buffer
	if _, err := io.Copy(&buf, content); err != nil {
		return err
	}

	// Store the file in the remote cache
	err = rw.remote.Set(ctx, path, key, bytes.NewReader(buf.Bytes()))

	return err
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
func (rw *RemoteWrapper) Exists(ctx context.Context, path string, key string) bool {
	// Check if the file exists in the local file system cache
	if rw.fs.Exists(ctx, path, key) {
		return true
	}

	// Check if the file exists in the remote cache
	return rw.remote.Exists(ctx, path, key)
}

// Clear removes all files from both the local file system cache and the remote cache.
func (rw *RemoteWrapper) Clear(ctx context.Context, expunge bool) error {
	// Clear the local file system cache
	err := rw.fs.Clear(ctx, expunge)
	if err != nil {
		return err
	}

	// Clear the remote cache
	err = rw.remote.Clear(ctx, expunge)
	return err
}
