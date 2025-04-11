package caching

import (
	"go.uber.org/zap"
	"grog/internal/config"
	"os"
	"path/filepath"
	"sync"
)

// FileSystemCache implements the Cache interface using the file system for storage
type FileSystemCache struct {
	workspaceRootDir string
	mutex            sync.RWMutex
	logger           *zap.SugaredLogger
}

func (fsc *FileSystemCache) TypeName() string {
	return "fs"
}

// NewFileSystemCache creates a new cache using the configured cache directory
func NewFileSystemCache(logger *zap.SugaredLogger) (*FileSystemCache, error) {
	workspaceDir := config.Global.WorkspaceRoot
	cacheDirName := config.GetWorkspaceCacheDirectoryName(workspaceDir)
	workspaceCacheDir := filepath.Join(config.Global.GetCacheDirectory(), cacheDirName)

	// Ensure the root directory exists
	if err := os.MkdirAll(workspaceCacheDir, 0755); err != nil {
		return nil, err
	}

	logger.Debugf("Instantiated fs cache at: %s", workspaceCacheDir)
	return &FileSystemCache{
		logger:           logger,
		workspaceRootDir: workspaceCacheDir,
		mutex:            sync.RWMutex{},
	}, nil
}

// buildFilePath constructs the full file path for a cached item
func (fsc *FileSystemCache) buildFilePath(path, key string) string {
	dir := filepath.Join(fsc.workspaceRootDir, path)
	return filepath.Join(dir, key)
}

// Get retrieves a cached file as a byte slice by its key
func (fsc *FileSystemCache) Get(path, key string) ([]byte, bool) {
	fsc.logger.Debugf("Getting file from cache for path: %s, key: %s", path, key)
	fsc.mutex.RLock()
	defer fsc.mutex.RUnlock()

	filePath := fsc.buildFilePath(path, key)

	data, err := os.ReadFile(filePath)
	if err != nil {
		fsc.logger.Debugf("Failed to get file for path: %s, key: %s", path, key)
		return nil, false
	}

	return data, true
}

// Set stores a file in the cache with the given key and content
func (fsc *FileSystemCache) Set(path, key string, content []byte) error {
	fsc.logger.Debugf("Setting file in cache for path: %s, key: %s", path, key)
	fsc.mutex.Lock()
	defer fsc.mutex.Unlock()

	// Make sure the directory exists
	dir := filepath.Join(fsc.workspaceRootDir, path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}

	filePath := fsc.buildFilePath(path, key)
	return os.WriteFile(filePath, content, 0644)
}

// Delete removes a cached file by its key
func (fsc *FileSystemCache) Delete(path, key string) error {
	fsc.logger.Debugf("Deleting file from cache for path: %s, key: %s", path, key)
	fsc.mutex.Lock()
	defer fsc.mutex.Unlock()

	filePath := fsc.buildFilePath(path, key)

	// Check if file exists before attempting to remove
	if _, err := os.Stat(filePath); os.IsNotExist(err) {
		fsc.logger.Debugf("File not found for deletion for path: %s, key: %s", path, key)
		return nil
	}

	return os.Remove(filePath)
}

// Exists checks if a file exists in the cache with the given key
func (fsc *FileSystemCache) Exists(path, key string) bool {
	fsc.logger.Debugf("Checking existence of file in cache for path: %s, key: %s", path, key)
	fsc.mutex.RLock()
	defer fsc.mutex.RUnlock()

	filePath := fsc.buildFilePath(path, key)

	_, err := os.Stat(filePath)
	fsc.logger.Debugf("File exists: %v", err == nil)
	return err == nil
}

// Clear removes all files from the cache
func (fsc *FileSystemCache) Clear(expunge bool) error {
	fsc.logger.Debugf("Clearing all files from cache expunge=%t", expunge)
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

	return os.RemoveAll(fsc.workspaceRootDir)
}
