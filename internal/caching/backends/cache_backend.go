package backends

import (
	"context"
	"grog/internal/config"
	"io"
)

// CacheBackend represents an interface for a file system-based cache.
type CacheBackend interface {
	// TypeName returns the name of the cache type.
	TypeName() string

	// Get retrieves a cached file as a byte slice by its key.
	// It returns the file content and a boolean indicating whether the key exists in the cache.
	Get(ctx context.Context, path, key string) (io.ReadCloser, error)

	// Set stores a file in the cache with the given key and content.
	// It overwrites the content if the key already exists.
	Set(ctx context.Context, path, key string, content io.Reader) error

	// Delete removes a cached file by its key.
	// It does nothing if the key does not exist.
	Delete(ctx context.Context, path string, key string) error

	// Exists checks if a file exists in the cache with the given key.
	Exists(ctx context.Context, path string, key string) (bool, error)
}

func GetCacheBackend(
	ctx context.Context,
	cacheConfig config.CacheConfig,
) (CacheBackend, error) {
	fs, err := NewFileSystemCache(ctx)
	if err != nil {
		return nil, err
	}

	switch cacheConfig.Backend {

	case config.GCSCacheBackend:
		gcsCache, err := NewGCSCache(ctx, cacheConfig.GCS)
		if err != nil {
			return nil, err
		}
		return NewRemoteWrapper(fs, gcsCache), nil
	case config.S3CacheBackend:
		s3Cache, err := NewS3Cache(ctx, cacheConfig.S3)
		if err != nil {
			return nil, err
		}
		return NewRemoteWrapper(fs, s3Cache), nil

	default:
		return fs, nil
	}
}
