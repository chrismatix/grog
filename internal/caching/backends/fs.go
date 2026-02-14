package backends

import (
	"context"
	"grog/internal/config"
	"grog/internal/console"
	"io"
	"os"
	"path/filepath"
)

// FileSystemCache implements the CacheBackend interface using the file system for storage
type FileSystemCache struct {
	workspaceCacheDir string
	sharedCasDir      string
}

func (fsc *FileSystemCache) TypeName() string {
	return "fs"
}

// NewFileSystemCache creates a new cache using the configured cache directory
func NewFileSystemCache(ctx context.Context) (*FileSystemCache, error) {
	workspaceCacheDir := config.Global.GetWorkspaceCacheDirectory()
	sharedCasDir := config.Global.GetCasDirectory()

	// Ensure the root directory exists
	if err := os.MkdirAll(workspaceCacheDir, 0755); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(sharedCasDir, 0755); err != nil {
		return nil, err
	}

	console.GetLogger(ctx).Tracef("Instantiated fs cache at: %s", workspaceCacheDir)
	return &FileSystemCache{
		workspaceCacheDir: workspaceCacheDir,
		sharedCasDir:      sharedCasDir,
	}, nil
}

// buildFilePath constructs the full file path for a cached item
func (fsc *FileSystemCache) buildFilePath(path, key string) string {
	dir := filepath.Join(fsc.getBaseDir(path), path)
	return filepath.Join(dir, key)
}

func (fsc *FileSystemCache) getBaseDir(path string) string {
	if path == "cas" {
		return fsc.sharedCasDir
	}
	return fsc.workspaceCacheDir
}

// Get retrieves a cached file by its key
func (fsc *FileSystemCache) Get(ctx context.Context, path, key string) (io.ReadCloser, error) {
	logger := console.GetLogger(ctx)
	logger.Tracef("Getting file from cache for path: %s, key: %s", path, key)

	filePath := fsc.buildFilePath(path, key)

	file, err := os.Open(filePath)
	if err != nil {
		logger.Tracef("Failed to get file for path: %s, key: %s", path, key)
		return nil, err
	}

	return file, err
}

// Set stores a file in the cache with the given key and content
func (fsc *FileSystemCache) Set(ctx context.Context, path, key string, content io.Reader) error {
	logger := console.GetLogger(ctx)
	logger.Tracef("Setting file in cache for path: %s, key: %s", path, key)

	// Make sure the directory exists
	dir := filepath.Join(fsc.getBaseDir(path), path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}

	// Write to a temp file first to ensure atomicity
	tmpFile, err := os.CreateTemp(dir, "tmp-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmpFile.Name()) // Cleanup temp file if rename fails

	// Copy the content from the reader to the file
	if _, err = io.Copy(tmpFile, content); err != nil {
		tmpFile.Close()
		return err
	}

	// Close explicitly before rename
	if err := tmpFile.Close(); err != nil {
		return err
	}

	filePath := fsc.buildFilePath(path, key)
	return os.Rename(tmpFile.Name(), filePath)
}

// Delete removes a cached file by its key
func (fsc *FileSystemCache) Delete(ctx context.Context, path, key string) error {
	logger := console.GetLogger(ctx)
	logger.Tracef("Deleting file from cache for path: %s, key: %s", path, key)

	filePath := fsc.buildFilePath(path, key)

	// Check if file exists before attempting to remove
	if _, err := os.Stat(filePath); os.IsNotExist(err) {
		logger.Tracef("File not found for deletion for path: %s, key: %s", path, key)
		return nil
	}

	return os.Remove(filePath)
}

// Exists checks if a file exists in the cache with the given key
func (fsc *FileSystemCache) Exists(ctx context.Context, path, key string) (bool, error) {
	logger := console.GetLogger(ctx)

	filePath := fsc.buildFilePath(path, key)

	_, err := os.Stat(filePath)
	if err != nil {
		if os.IsNotExist(err) {
			logger.Tracef("Cache-miss for path: %s, key: %s", path, key)
			return false, nil
		}
		logger.Tracef("Cache failed for path: %s, key: %s %v", path, key, err)
		return false, err
	}
	logger.Tracef("Cache-hit for path: %s, key: %s", path, key)
	return true, nil
}

func (fsc *FileSystemCache) GetWorkspaceCacheSizeBytes() (int64, error) {
	return getDirectorySizeBytes(fsc.workspaceCacheDir)
}

func (fsc *FileSystemCache) GetCacheSizeBytes() (int64, error) {
	return getDirectorySizeBytes(config.Global.Root)
}

func getDirectorySizeBytes(path string) (int64, error) {
	var size int64
	err := filepath.Walk(path, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !info.IsDir() {
			size += info.Size()
		}
		return nil
	})

	return size, err
}
