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

	// BeginWrite opens a streaming writer whose final cache location is
	// decided at Commit time. This is used by callers that compute the cache
	// key from the streamed bytes themselves (e.g. the dockerproxy registry,
	// which only learns the digest from the daemon's PUT request after every
	// chunk has already been received).
	//
	// Implementations MUST guarantee that data written to the returned
	// StagedWriter is invisible to Get/Exists/Size against any cache key
	// until Commit returns successfully. Backends that store data on disk
	// should stage in a directory on the same filesystem as the final
	// destination so the commit step can be a single rename.
	//
	// Either Commit or Cancel must be called on the returned writer to
	// release backend resources. Cancel is idempotent and safe to call
	// after a successful Commit.
	BeginWrite(ctx context.Context) (StagedWriter, error)

	// Delete removes a cached file by its key.
	// It does nothing if the key does not exist.
	Delete(ctx context.Context, path string, key string) error

	// Exists checks if a file exists in the cache with the given key.
	Exists(ctx context.Context, path string, key string) (bool, error)

	// Size returns the byte size of the entry without reading its content.
	// Used by the dockerproxy registry to populate Content-Length headers on
	// blob HEAD/GET responses (the Docker daemon refuses responses without one).
	// Returns an error if the entry does not exist.
	Size(ctx context.Context, path, key string) (int64, error)

	// ListKeys returns all keys under the given path that match the given suffix.
	// Keys are returned as relative paths (e.g. "2026-03-30/trace-id.parquet").
	ListKeys(ctx context.Context, path string, suffix string) ([]string, error)
}

// StagedWriter accumulates bytes that will be promoted to a cache entry only
// once Commit is called with the final (path, key). It is the streaming
// counterpart to CacheBackend.Set: callers that don't know the key upfront
// (e.g. content-addressed writes that derive the key from a hash of the bytes)
// can stream straight into a StagedWriter and decide where it lands later.
//
// StagedWriter is not safe for concurrent Write calls; the caller must
// serialise writes if multiple goroutines need to push bytes. Commit and
// Cancel may be called from any goroutine.
type StagedWriter interface {
	io.Writer

	// Commit promotes the staged data to (path, key) and releases backend
	// resources. After a successful Commit the data is visible to subsequent
	// Get/Exists/Size against the same (path, key). Commit must not be called
	// more than once. After a failed Commit the writer should be Cancel'd.
	Commit(ctx context.Context, path, key string) error

	// Cancel discards the staged data without making it visible at any cache
	// key. Cancel is idempotent and safe to call after Commit (in which case
	// it is a no-op). Callers should defer Cancel right after BeginWrite so
	// any error path releases resources without further bookkeeping.
	Cancel(ctx context.Context) error
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
	case config.AzureCacheBackend:
		azureCache, err := NewAzureCache(ctx, cacheConfig.Azure)
		if err != nil {
			return nil, err
		}
		return NewRemoteWrapper(fs, azureCache), nil

	default:
		return fs, nil
	}
}
