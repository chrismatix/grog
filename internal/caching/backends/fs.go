package backends

import (
	"context"
	"grog/internal/config"
	"grog/internal/console"
	"io"
	"os"
	"path/filepath"
	"sync"
)

// FileSystemCache implements the CacheBackend interface using the file system for storage
type FileSystemCache struct {
	workspaceCacheDir string
	// TODO is there a better way of making this thread-safe perhaps on a file level?
	mutex sync.RWMutex
}

func (fsc *FileSystemCache) TypeName() string {
	return "fs"
}

// NewFileSystemCache creates a new cache using the configured cache directory
func NewFileSystemCache(ctx context.Context) (*FileSystemCache, error) {
	workspaceDir := config.Global.WorkspaceRoot
	workspacePrefix := config.GetWorkspaceCachePrefix(workspaceDir)
	workspaceCacheDir := filepath.Join(config.Global.GetCacheDirectory(), workspacePrefix)

	// Ensure the root directory exists
	if err := os.MkdirAll(workspaceCacheDir, 0755); err != nil {
		return nil, err
	}

	console.GetLogger(ctx).Debugf("Instantiated fs cache at: %s", workspaceCacheDir)
	return &FileSystemCache{
		workspaceCacheDir: workspaceCacheDir,
		mutex:             sync.RWMutex{},
	}, nil
}

// buildFilePath constructs the full file path for a cached item
func (fsc *FileSystemCache) buildFilePath(path, key string) string {
	dir := filepath.Join(fsc.workspaceCacheDir, path)
	return filepath.Join(dir, key)
}

// Get retrieves a cached file by its key
func (fsc *FileSystemCache) Get(ctx context.Context, path, key string) (io.ReadCloser, error) {
	logger := console.GetLogger(ctx)
	logger.Debugf("Getting file from cache for path: %s, key: %s", path, key)
	fsc.mutex.RLock()
	defer fsc.mutex.RUnlock()

	filePath := fsc.buildFilePath(path, key)

	file, err := os.Open(filePath)
	if err != nil {
		logger.Debugf("Failed to get file for path: %s, key: %s", path, key)
		return nil, err
	}

	return file, err
}

// Set stores a file in the cache with the given key and content
func (fsc *FileSystemCache) Set(ctx context.Context, path, key string, content io.Reader) error {
	logger := console.GetLogger(ctx)
	logger.Debugf("Setting file in cache for path: %s, key: %s", path, key)

	// Use Lock for writing operations
	fsc.mutex.Lock()
	defer fsc.mutex.Unlock()

	// Make sure the directory exists
	dir := filepath.Join(fsc.workspaceCacheDir, path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}

	filePath := fsc.buildFilePath(path, key)

	file, err := os.Create(filePath)
	if err != nil {
		return err
	}
	defer file.Close()

	// Copy the content from the reader to the file, respecting context cancellation
	_, err = io.Copy(file, content)
	if err != nil {
		// If there was an error, attempt to remove the partially written file
		os.Remove(filePath)
		return err
	}

	return nil
}

// Delete removes a cached file by its key
func (fsc *FileSystemCache) Delete(ctx context.Context, path, key string) error {
	logger := console.GetLogger(ctx)
	logger.Debugf("Deleting file from cache for path: %s, key: %s", path, key)
	fsc.mutex.Lock()
	defer fsc.mutex.Unlock()

	filePath := fsc.buildFilePath(path, key)

	// Check if file exists before attempting to remove
	if _, err := os.Stat(filePath); os.IsNotExist(err) {
		logger.Debugf("File not found for deletion for path: %s, key: %s", path, key)
		return nil
	}

	return os.Remove(filePath)
}

// Exists checks if a file exists in the cache with the given key
func (fsc *FileSystemCache) Exists(ctx context.Context, path, key string) (bool, error) {
	logger := console.GetLogger(ctx)
	logger.Debugf("Checking existence of file in cache for path: %s, key: %s", path, key)
	fsc.mutex.RLock()
	defer fsc.mutex.RUnlock()

	filePath := fsc.buildFilePath(path, key)

	_, err := os.Stat(filePath)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

// Clear removes all files from the cache
func (fsc *FileSystemCache) Clear(ctx context.Context, expunge bool) error {
	logger := console.GetLogger(ctx)
	logger.Debugf("Clearing all files from cache expunge=%t", expunge)
	fsc.mutex.Lock()
	defer fsc.mutex.Unlock()

	if expunge {
		cacheDir := config.Global.GetCacheDirectory()
		// Remove the entire cache directory
		err := os.RemoveAll(cacheDir)
		if err != nil {
			return err
		}

		// Recreate the empty root directory
		return os.MkdirAll(cacheDir, 0755)
	}

	return os.RemoveAll(fsc.workspaceCacheDir)
}
