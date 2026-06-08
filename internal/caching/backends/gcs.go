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

// GCSClient defines the operations GCSCache needs against Google Cloud Storage.
// Wrapping the SDK behind an interface lets the cache logic be unit-tested
// against a mock, matching the pattern S3Client / AzureBlobClient already use.
type GCSClient interface {
	// NewReader opens a streaming reader for an object.
	NewReader(ctx context.Context, bucket, object string) (io.ReadCloser, error)

	// NewWriter opens a streaming writer for an object. The returned writer must
	// be Closed to finalize the upload.
	NewWriter(ctx context.Context, bucket, object string) io.WriteCloser

	// Delete removes the named object.
	Delete(ctx context.Context, bucket, object string) error

	// Attrs returns the object's size, returning storage.ErrObjectNotExist if
	// the object is absent.
	Attrs(ctx context.Context, bucket, object string) (size int64, err error)

	// Copy copies srcObject to dstObject within the same bucket.
	Copy(ctx context.Context, bucket, srcObject, dstObject string) error

	// List enumerates object names matching prefix; the iterator yields
	// iterator.Done when exhausted.
	List(ctx context.Context, bucket, prefix string) GCSObjectIterator
}

// GCSObjectIterator yields object names; satisfied by *storage.ObjectIterator.
type GCSObjectIterator interface {
	// NextName returns the next object's name or iterator.Done when exhausted.
	NextName() (string, error)
}

// GCSStorageAdapter adapts cloud.google.com/go/storage to GCSClient.
type GCSStorageAdapter struct {
	client *storage.Client
}

// NewGCSStorageAdapter creates a new adapter for the cloud.google.com/go/storage client.
func NewGCSStorageAdapter(client *storage.Client) *GCSStorageAdapter {
	return &GCSStorageAdapter{client: client}
}

func (a *GCSStorageAdapter) NewReader(ctx context.Context, bucket, object string) (io.ReadCloser, error) {
	return a.client.Bucket(bucket).Object(object).NewReader(ctx)
}

func (a *GCSStorageAdapter) NewWriter(ctx context.Context, bucket, object string) io.WriteCloser {
	return a.client.Bucket(bucket).Object(object).NewWriter(ctx)
}

func (a *GCSStorageAdapter) Delete(ctx context.Context, bucket, object string) error {
	return a.client.Bucket(bucket).Object(object).Delete(ctx)
}

func (a *GCSStorageAdapter) Attrs(ctx context.Context, bucket, object string) (int64, error) {
	attrs, err := a.client.Bucket(bucket).Object(object).Attrs(ctx)
	if err != nil {
		return 0, err
	}
	return attrs.Size, nil
}

func (a *GCSStorageAdapter) Copy(ctx context.Context, bucket, srcObject, dstObject string) error {
	src := a.client.Bucket(bucket).Object(srcObject)
	dst := a.client.Bucket(bucket).Object(dstObject)
	_, err := dst.CopierFrom(src).Run(ctx)
	return err
}

func (a *GCSStorageAdapter) List(ctx context.Context, bucket, prefix string) GCSObjectIterator {
	return &gcsIteratorAdapter{it: a.client.Bucket(bucket).Objects(ctx, &storage.Query{Prefix: prefix})}
}

type gcsIteratorAdapter struct {
	it *storage.ObjectIterator
}

func (g *gcsIteratorAdapter) NextName() (string, error) {
	attrs, err := g.it.Next()
	if err != nil {
		return "", err
	}
	return attrs.Name, nil
}

// GCSCache implements the CacheBackend interface using Google Cloud Storage.
type GCSCache struct {
	bucketName      string
	prefix          string
	workspacePrefix string
	client          GCSClient
	logger          *console.Logger
}

func (gcs *GCSCache) TypeName() string {
	return "gcs"
}

// NewGCSCache creates a new GCS cache backed by the real Google Cloud Storage client.
func NewGCSCache(
	ctx context.Context,
	cacheConfig config.GCSCacheConfig,
) (*GCSCache, error) {
	if cacheConfig.Bucket == "" {
		return nil, fmt.Errorf("GCS bucket name is not set")
	}

	storageClient, err := storage.NewClient(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to create client: %w", err)
	}
	adapter := NewGCSStorageAdapter(storageClient)
	return NewGCSCacheWithClient(ctx, cacheConfig, adapter)
}

// NewGCSCacheWithClient creates a new GCS cache with a provided client.
// This is useful for testing with a mock client.
func NewGCSCacheWithClient(
	ctx context.Context,
	cacheConfig config.GCSCacheConfig,
	client GCSClient,
) (*GCSCache, error) {
	if cacheConfig.Bucket == "" {
		return nil, fmt.Errorf("GCS bucket name is not set")
	}

	prefix := cacheConfig.Prefix
	if prefix != "" {
		prefix = strings.Trim(prefix, "/")
	}

	workspaceDir := config.Global.WorkspaceRoot
	var workspacePrefix string

	if cacheConfig.SharedCache {
		workspacePrefix = ""
	} else {
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

	rc, err := gcs.client.NewReader(ctx, gcs.bucketName, gcsPath)
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

	wc := gcs.client.NewWriter(ctx, gcs.bucketName, gcsPath)
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

	if err := gcs.client.Delete(ctx, gcs.bucketName, gcsPath); err != nil {
		return fmt.Errorf("failed to delete object: %w", err)
	}
	return nil
}

// BeginWrite opens a streaming upload to a staging object under .uploads/<uuid>.
// On Commit, the staging object is server-side copied to its final
// content-addressed key and the staging object is deleted. No bytes pass
// through grog twice.
//
// The ctx passed here lives for the entire upload session — it is captured
// inside the writer that drives the resumable upload — so callers must pass a
// context that outlives all subsequent Write calls (e.g. the ociproxy
// registry's session ctx, not an HTTP request ctx).
func (gcs *GCSCache) BeginWrite(ctx context.Context) (StagedWriter, error) {
	stagingKey := gcs.buildPath(gcsStagingPath, uuid.NewString())
	wc := gcs.client.NewWriter(ctx, gcs.bucketName, stagingKey)
	return &gcsStagedWriter{
		client:     gcs.client,
		bucket:     gcs.bucketName,
		stagingKey: stagingKey,
		writer:     wc,
		buildPath:  gcs.buildPath,
	}, nil
}

// gcsStagedWriter is the StagedWriter implementation for GCS. The streaming
// resumable upload is owned by the underlying writer (which captures the ctx
// from BeginWrite internally); Commit promotes the staging object via a
// server-side copy.
type gcsStagedWriter struct {
	client     GCSClient
	bucket     string
	stagingKey string
	writer     io.WriteCloser
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

	if err := w.writer.Close(); err != nil {
		_ = w.cleanupStaging(ctx)
		return fmt.Errorf("close staging writer: %w", err)
	}

	finalKey := w.buildPath(path, key)
	if err := w.client.Copy(ctx, w.bucket, w.stagingKey, finalKey); err != nil {
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

	_ = w.writer.Close()
	return w.cleanupStaging(ctx)
}

func (w *gcsStagedWriter) cleanupStaging(ctx context.Context) error {
	return w.client.Delete(ctx, w.bucket, w.stagingKey)
}

// Size returns the byte size of an object in GCS via Object.Attrs (a metadata
// fetch — no body is downloaded).
func (gcs *GCSCache) Size(ctx context.Context, path, key string) (int64, error) {
	logger := console.GetLogger(ctx)
	gcsPath := gcs.buildPath(path, key)
	logger.Tracef("Sizing object in GCS for path: %s", gcsPath)

	size, err := gcs.client.Attrs(ctx, gcs.bucketName, gcsPath)
	if err != nil {
		return 0, fmt.Errorf("failed to get object attrs: %w", err)
	}
	return size, nil
}

// Exists checks if a file exists in GCS.
func (gcs *GCSCache) Exists(ctx context.Context, path string, key string) (bool, error) {
	logger := console.GetLogger(ctx)
	gcsPath := gcs.buildPath(path, key)
	logger.Tracef("Checking existence of file in GCS for path: %s", gcsPath)

	_, err := gcs.client.Attrs(ctx, gcs.bucketName, gcsPath)
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

	it := gcs.client.List(ctx, gcs.bucketName, fullPath)

	var keys []string
	for {
		name, err := it.NextName()
		if err == iterator.Done {
			break
		}
		if err != nil {
			return nil, err
		}
		key := strings.TrimPrefix(name, fullPath)
		if suffix == "" || strings.HasSuffix(key, suffix) {
			keys = append(keys, key)
		}
	}
	return keys, nil
}
