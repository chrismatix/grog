package cache

import (
	"os"
	"path/filepath"
	"sync"
)

// FileSystemCache implements the Cache interface using the file system for storage
type FileSystemCache struct {
	rootDir string
	mutex   sync.RWMutex
}

// NewFileSystemCache creates a new cache with the specified root directory
func NewFileSystemCache(rootDir string) (*FileSystemCache, error) {
	// Ensure the root directory exists
	if err := os.MkdirAll(rootDir, 0755); err != nil {
		return nil, err
	}

	return &FileSystemCache{
		rootDir: rootDir,
		mutex:   sync.RWMutex{},
	}, nil
}

// buildFilePath constructs the full file path for a cached item
func (c *FileSystemCache) buildFilePath(path, key string) string {
	dir := filepath.Join(c.rootDir, path)
	return filepath.Join(dir, key)
}

// Get retrieves a cached file as a byte slice by its key
func (c *FileSystemCache) Get(path, key string) ([]byte, bool) {
	c.mutex.RLock()
	defer c.mutex.RUnlock()

	filePath := c.buildFilePath(path, key)

	data, err := os.ReadFile(filePath)
	if err != nil {
		return nil, false
	}

	return data, true
}

// Set stores a file in the cache with the given key and content
func (c *FileSystemCache) Set(path, key string, content []byte) error {
	c.mutex.Lock()
	defer c.mutex.Unlock()

	// Make sure the directory exists
	dir := filepath.Join(c.rootDir, path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}

	filePath := c.buildFilePath(path, key)
	return os.WriteFile(filePath, content, 0644)
}

// Delete removes a cached file by its key
func (c *FileSystemCache) Delete(path, key string) error {
	c.mutex.Lock()
	defer c.mutex.Unlock()

	filePath := c.buildFilePath(path, key)

	// Check if file exists before attempting to remove
	if _, err := os.Stat(filePath); os.IsNotExist(err) {
		return nil // File doesn't exist, so nothing to delete
	}

	return os.Remove(filePath)
}

// Exists checks if a file exists in the cache with the given key
func (c *FileSystemCache) Exists(path, key string) bool {
	c.mutex.RLock()
	defer c.mutex.RUnlock()

	filePath := c.buildFilePath(path, key)

	_, err := os.Stat(filePath)
	return err == nil
}

// Clear removes all files from the cache
func (c *FileSystemCache) Clear() error {
	c.mutex.Lock()
	defer c.mutex.Unlock()

	// Remove all content from the root directory
	err := os.RemoveAll(c.rootDir)
	if err != nil {
		return err
	}

	// Recreate the empty root directory
	return os.MkdirAll(c.rootDir, 0755)
}
