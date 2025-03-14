package cache

// Cache represents an interface for a file system-based cache.
type Cache interface {
	// Get retrieves a cached file as a byte slice by its key.
	// It returns the file content and a boolean indicating whether the key exists in the cache.
	Get(key string) ([]byte, bool)

	// Set stores a file in the cache with the given key and content.
	// It overwrites the content if the key already exists.
	Set(key string, content []byte) error

	// Delete removes a cached file by its key.
	// It does nothing if the key does not exist.
	Delete(key string) error

	// Exists checks if a file exists in the cache with the given key.
	Exists(key string) bool

	// Clear removes all files from the cache.
	Clear() error
}
