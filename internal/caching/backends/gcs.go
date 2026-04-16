package backends

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"

	"cloud.google.com/go/storage"
	"github.com/google/uuid"
	"google.golang.org/api/iterator"

	"grog/internal/config"
	"grog/internal/console"
)

// gcsStagingPath is the path used for in-flight uploads. Anything under this
// prefix is staged data and must be invisible to Get/Exists/Size against any
// cache key.
const gcsStagingPath = ".uploads"

// GCSCache implements the CacheBackend interface using Google Cloud Storage.
type GCSCache struct {
	bucketName      string
	prefix          string
	workspacePrefix string
	client          *storage.Client
	logger          *console.Logger
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
		// If shared cache is enabled treat prefix as the workspace root
		workspacePrefix = ""
	} else {
		// If shared cache is disabled, use the full path hash
		workspacePrefix = strings.Trim(config.GetWorkspaceCachePrefix(workspaceDir), "/")
	}

	logger := console.GetLogger(ctx)
	logger.Tracef("Instantiated GCS cache at bucket %s with prefix %s and workspace dir %s",
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
	if gcs.workspacePrefix == "" {
		return gcs.prefix
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
	logger.Tracef("Getting file from GCS for path: %s", gcsPath)

	rc, err := gcs.client.Bucket(gcs.bucketName).Object(gcsPath).NewReader(ctx)
	if err != nil {
		logger.Tracef("Failed to get file from GCS for path: %s, key: %s: %v", path, key, err)
		return nil, fmt.Errorf("failed to create reader: %w", err)
	}

	return rc, nil
}

// Set stores a file in GCS.
func (gcs *GCSCache) Set(ctx context.Context, path, key string, content io.Reader) error {
	logger := console.GetLogger(ctx)
	gcsPath := gcs.buildPath(path, key)
	logger.Tracef("Setting file in GCS for path: %s", gcsPath)

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
	logger.Tracef("Deleting file from GCS for path: %s", gcsPath)

	err := gcs.client.Bucket(gcs.bucketName).Object(gcsPath).Delete(ctx)
	if err != nil {
		return fmt.Errorf("failed to delete object: %w", err)
	}

	return nil
}

// BeginWrite opens a streaming upload to a staging object under .uploads/<uuid>.
// On Commit, the staging object is server-side copied to its final
// content-addressed key via Object.CopierFrom and the staging object is
// deleted. No bytes pass through grog twice.
//
// The ctx passed here lives for the entire upload session — it's captured
// inside *storage.Writer which uses it to drive the resumable upload — so
// callers must pass a context that outlives all subsequent Write calls
// (e.g. the dockerproxy registry's session ctx, not an HTTP request ctx).
func (gcs *GCSCache) BeginWrite(ctx context.Context) (StagedWriter, error) {
	stagingKey := gcs.buildPath(gcsStagingPath, uuid.NewString())
	wc := gcs.client.Bucket(gcs.bucketName).Object(stagingKey).NewWriter(ctx)
	return &gcsStagedWriter{
		client:     gcs.client,
		bucket:     gcs.bucketName,
		stagingKey: stagingKey,
		writer:     wc,
		buildPath:  gcs.buildPath,
	}, nil
}

// gcsStagedWriter is the StagedWriter implementation for GCS. The streaming
// resumable upload is owned by *storage.Writer (which captures the ctx from
// BeginWrite internally); Commit promotes the staging object via a
// server-side rewrite using Object.CopierFrom.
type gcsStagedWriter struct {
	client     *storage.Client
	bucket     string
	stagingKey string
	writer     *storage.Writer
	buildPath  func(path, key string) string

	mu       sync.Mutex
	finished bool
}

func (w *gcsStagedWriter) Write(p []byte) (int, error) {
	return w.writer.Write(p)
}

func (w *gcsStagedWriter) Commit(ctx context.Context, path, key string) error {
	w.mu.Lock()
	if w.finished {
		w.mu.Unlock()
		return errors.New("gcs staged writer: commit after commit/cancel")
	}
	w.finished = true
	w.mu.Unlock()

	// Closing the writer flushes and finalizes the resumable upload.
	if err := w.writer.Close(); err != nil {
		_ = w.cleanupStaging(ctx)
		return fmt.Errorf("close staging writer: %w", err)
	}

	finalKey := w.buildPath(path, key)
	src := w.client.Bucket(w.bucket).Object(w.stagingKey)
	dst := w.client.Bucket(w.bucket).Object(finalKey)
	if _, err := dst.CopierFrom(src).Run(ctx); err != nil {
		_ = w.cleanupStaging(ctx)
		return fmt.Errorf("copy staging -> final: %w", err)
	}

	_ = w.cleanupStaging(ctx)
	return nil
}

func (w *gcsStagedWriter) Cancel(ctx context.Context) error {
	w.mu.Lock()
	if w.finished {
		w.mu.Unlock()
		return nil
	}
	w.finished = true
	w.mu.Unlock()

	// Aborting the resumable upload — *storage.Writer doesn't expose a
	// dedicated abort, but Close on a writer that hasn't been fully written
	// to ends the upload session. Any partial object is deleted below.
	_ = w.writer.Close()
	return w.cleanupStaging(ctx)
}

func (w *gcsStagedWriter) cleanupStaging(ctx context.Context) error {
	if err := w.client.Bucket(w.bucket).Object(w.stagingKey).Delete(ctx); err != nil {
		// Not fatal — orphan staging objects can be cleaned up by a GCS
		// lifecycle rule on the .uploads/ prefix.
		return err
	}
	return nil
}

// Size returns the byte size of an object in GCS via Object.Attrs (a metadata
// fetch — no body is downloaded).
func (gcs *GCSCache) Size(ctx context.Context, path, key string) (int64, error) {
	logger := console.GetLogger(ctx)
	gcsPath := gcs.buildPath(path, key)
	logger.Tracef("Sizing object in GCS for path: %s", gcsPath)

	attrs, err := gcs.client.Bucket(gcs.bucketName).Object(gcsPath).Attrs(ctx)
	if err != nil {
		return 0, fmt.Errorf("failed to get object attrs: %w", err)
	}
	return attrs.Size, nil
}

// Exists checks if a file exists in GCS.
func (gcs *GCSCache) Exists(ctx context.Context, path string, key string) (bool, error) {
	logger := console.GetLogger(ctx)
	gcsPath := gcs.buildPath(path, key)
	logger.Tracef("Checking existence of file in GCS for path: %s", gcsPath)

	_, err := gcs.client.Bucket(gcs.bucketName).Object(gcsPath).Attrs(ctx)
	if errors.Is(err, storage.ErrObjectNotExist) {
		logger.Tracef("File does not exist: %s", gcsPath)
		return false, nil
	}
	if err != nil {
		return false, err
	}

	return true, nil
}

// ListKeys uses GCS Objects.List to list keys under the given path.
func (gcs *GCSCache) ListKeys(ctx context.Context, path string, suffix string) ([]string, error) {
	fullPath := gcs.buildPath(path, "")
	if !strings.HasSuffix(fullPath, "/") {
		fullPath += "/"
	}

	it := gcs.client.Bucket(gcs.bucketName).Objects(ctx, &storage.Query{
		Prefix: fullPath,
	})

	var keys []string
	for {
		attrs, err := it.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			return nil, err
		}
		key := strings.TrimPrefix(attrs.Name, fullPath)
		if suffix == "" || strings.HasSuffix(key, suffix) {
			keys = append(keys, key)
		}
	}
	return keys, nil
}
