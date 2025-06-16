package backends

import (
	"cloud.google.com/go/storage"
	"context"
	"errors"
	"fmt"
	"go.uber.org/zap"
	"grog/internal/config"
	"grog/internal/console"
	"io"
	"path/filepath"
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
		prefix = ""
	} else {
		prefix = strings.Trim(prefix, "/")
	}

	workspaceDir := config.Global.WorkspaceRoot
	var workspacePrefix string

	if cacheConfig.SharedCache {
		// If shared cache is enabled, use only the directory name
		workspacePrefix = filepath.Base(workspaceDir)
	} else {
		// If shared cache is disabled, use the full path hash
		workspacePrefix = strings.Trim(config.GetWorkspaceCachePrefix(workspaceDir), "/")
	}

	logger := console.GetLogger(ctx)
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
	if gcs.prefix == "" {
		return gcs.workspacePrefix
	}
	return gcs.prefix + "/" + gcs.workspacePrefix
}

// buildPath constructs the full GCS path for a cached item.
func (gcs *GCSCache) buildPath(path, key string) string {
	parts := []string{gcs.fullPrefix(), strings.Trim(path, "/"), strings.Trim(key, "/")}
	return strings.Join(parts, "/")
}

// Get retrieves a cached file from GCS.
func (gcs *GCSCache) Get(ctx context.Context, path, key string) (io.ReadCloser, error) {
	logger := console.GetLogger(ctx)
	gcsPath := gcs.buildPath(path, key)
	logger.Debugf("Getting file from GCS for path: %s", gcsPath)

	rc, err := gcs.client.Bucket(gcs.bucketName).Object(gcsPath).NewReader(ctx)
	if err != nil {
		logger.Debugf("Failed to get file from GCS for path: %s, key: %s: %v", path, key, err)
		return nil, fmt.Errorf("failed to create reader: %w", err)
	}

	return rc, nil
}

// Set stores a file in GCS.
func (gcs *GCSCache) Set(ctx context.Context, path, key string, content io.Reader) error {
	logger := console.GetLogger(ctx)
	gcsPath := gcs.buildPath(path, key)
	logger.Debugf("Setting file in GCS for path: %s", gcsPath)

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
	logger := console.GetLogger(ctx)
	gcsPath := gcs.buildPath(path, key)
	logger.Debugf("Deleting file from GCS for path: %s", gcsPath)

	err := gcs.client.Bucket(gcs.bucketName).Object(gcsPath).Delete(ctx)
	if err != nil {
		return fmt.Errorf("failed to delete object: %w", err)
	}

	return nil
}

// Exists checks if a file exists in GCS.
func (gcs *GCSCache) Exists(ctx context.Context, path string, key string) (bool, error) {
	logger := console.GetLogger(ctx)
	gcsPath := gcs.buildPath(path, key)
	logger.Debugf("Checking existence of file in GCS for path: %s", gcsPath)

	_, err := gcs.client.Bucket(gcs.bucketName).Object(gcsPath).Attrs(ctx)
	if errors.Is(err, storage.ErrObjectNotExist) {
		logger.Debugf("File does not exist: %s", gcsPath)
		return false, nil
	}
	if err != nil {
		return false, err
	}

	return true, nil
}
