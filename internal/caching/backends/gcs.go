package backends

import (
	"cloud.google.com/go/storage"
	"context"
	"errors"
	"fmt"
	"go.uber.org/zap"
	"grog/internal/config"
	"io"
	"strings"
)

// GCSCache implements the CacheBackend interface using Google Cloud Storage.
type GCSCache struct {
	bucketName      string
	prefix          string
	workspacePrefix string
	client          *storage.Client
	logger          *zap.SugaredLogger
}

func (gcs *GCSCache) TypeName() string {
	return "gcs"
}

// NewGCSCache creates a new GCS cache.
func NewGCSCache(
	ctx context.Context,
	logger *zap.SugaredLogger,
	cacheConfig config.GCSCacheConfig,
) (*GCSCache, error) {
	if cacheConfig.Bucket == "" {
		return nil, fmt.Errorf("GCS bucket name is not set")
	}

	client, err := storage.NewClient(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to create client: %w", err)
	}

	prefix := cacheConfig.Prefix
	if prefix == "" {
		prefix = "/"
	} else if !strings.HasPrefix(prefix, "/") {
		prefix = "/" + prefix
	}

	workspaceDir := config.Global.WorkspaceRoot
	workspacePrefix := config.GetWorkspaceCachePrefix(workspaceDir)

	logger.Debugf("Instantiated GCS cache at bucket %s with prefix %s and workspace dir %s",
		cacheConfig.Bucket,
		prefix,
		workspacePrefix)
	return &GCSCache{
		logger:          logger,
		client:          client,
		bucketName:      cacheConfig.Bucket,
		prefix:          prefix,
		workspacePrefix: workspacePrefix,
	}, nil
}

func (gcs *GCSCache) fullPrefix() string {
	return gcs.prefix + "/" + gcs.workspacePrefix
}

// buildPath constructs the full GCS path for a cached item.
func (gcs *GCSCache) buildPath(path, key string) string {
	// Avoid accidental double slashes
	return gcs.fullPrefix() + "/" + strings.TrimPrefix(path, "/") + "/" + strings.TrimPrefix(key, "/")
}

// Get retrieves a cached file from GCS.
func (gcs *GCSCache) Get(ctx context.Context, path, key string) (io.ReadCloser, error) {
	gcs.logger.Debugf("Getting file from GCS for path: %s, key: %s", path, key)

	gcsPath := gcs.buildPath(path, key)
	rc, err := gcs.client.Bucket(gcs.bucketName).Object(gcsPath).NewReader(ctx)
	if err != nil {
		gcs.logger.Debugf("Failed to get file from GCS for path: %s, key: %s: %v", path, key, err)
		return nil, fmt.Errorf("failed to create reader: %w", err)
	}

	return rc, nil
}

// Set stores a file in GCS.
func (gcs *GCSCache) Set(ctx context.Context, path, key string, content io.Reader) error {
	gcs.logger.Debugf("Setting file in GCS for path: %s, key: %s", path, key)

	gcsPath := gcs.buildPath(path, key)
	wc := gcs.client.Bucket(gcs.bucketName).Object(gcsPath).NewWriter(ctx)

	if _, err := io.Copy(wc, content); err != nil {
		return fmt.Errorf("failed to copy data to GCS: %w", err)
	}

	if err := wc.Close(); err != nil {
		return fmt.Errorf("failed to close writer: %w", err)
	}

	return nil
}

// Delete removes a cached file from GCS.
func (gcs *GCSCache) Delete(ctx context.Context, path string, key string) error {
	gcs.logger.Debugf("Deleting file from GCS for path: %s, key: %s", path, key)

	gcsPath := gcs.buildPath(path, key)

	err := gcs.client.Bucket(gcs.bucketName).Object(gcsPath).Delete(ctx)
	if err != nil {
		return fmt.Errorf("failed to delete object: %w", err)
	}

	return nil
}

// Exists checks if a file exists in GCS.
func (gcs *GCSCache) Exists(ctx context.Context, path string, key string) (bool, error) {
	gcs.logger.Debugf("Checking existence of file in GCS for path: %s, key: %s", path, key)

	gcsPath := gcs.buildPath(path, key)

	_, err := gcs.client.Bucket(gcs.bucketName).Object(gcsPath).Attrs(ctx)
	if errors.Is(err, storage.ErrObjectNotExist) {
		gcs.logger.Debugf("File does not exist: %s", gcsPath)
		return false, nil
	}
	if err != nil {
		return false, err
	}

	return true, nil
}

// Clear removes all files from the GCS bucket with the given prefix (path).
func (gcs *GCSCache) Clear(ctx context.Context, expunge bool) error {
	gcs.logger.Debugf("Clearing all files from GCS with expunge=%t", expunge)

	// Only delete the workspace cache files
	deletePrefix := gcs.fullPrefix()
	if expunge {
		// If expunge is true, delete all files under prefix
		deletePrefix = gcs.prefix
	}

	// List all objects with the prefix and delete them.
	query := &storage.Query{Prefix: deletePrefix}
	it := gcs.client.Bucket(gcs.bucketName).Objects(ctx, query)
	for {
		attrs, err := it.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("failed to list objects: %w", err)
		}

		if err := gcs.client.Bucket(gcs.bucketName).Object(attrs.Name).Delete(ctx); err != nil {
			return fmt.Errorf("failed to delete object %s: %w", attrs.Name, err)
		}
	}

	return nil
}
