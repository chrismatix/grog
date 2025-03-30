package cache

import (
	"github.com/spf13/viper"
	"grog/internal/config"
)

// Cache represents an interface for a file system-based cache.
type Cache interface {
	// Get retrieves a cached file as a byte slice by its key.
	// It returns the file content and a boolean indicating whether the key exists in the cache.
	Get(path string, key string) ([]byte, bool)

	// Set stores a file in the cache with the given key and content.
	// It overwrites the content if the key already exists.
	Set(path string, key string, content []byte) error

	// Delete removes a cached file by its key.
	// It does nothing if the key does not exist.
	Delete(path string, key string) error

	// Exists checks if a file exists in the cache with the given key.
	Exists(path string, key string) bool

	// Clear removes all files from the cache.
	Clear() error
}

func GetCache() (Cache, error) {
	cacheDir := viper.GetString("cache_dir")
	workspaceDir := viper.GetString("workspace_root")
	workspaceCacheDir := config.GetWorkspaceCacheDirectory(cacheDir, workspaceDir)

	return NewFileSystemCache(workspaceCacheDir)
}
