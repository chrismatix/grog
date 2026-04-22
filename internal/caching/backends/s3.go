package backends

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/feature/s3/manager"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/google/uuid"

	grogconfig "grog/internal/config"
	"grog/internal/console"
)

// S3Client defines the interface for S3 operations.
// This interface allows for easy mocking in tests.
type S3Client interface {
	// GetObject retrieves an object from S3.
	GetObject(ctx context.Context, bucket, key string) (io.ReadCloser, error)

	// PutObject uploads an object to S3.
	PutObject(ctx context.Context, bucket, key string, body io.Reader) error

	// DeleteObject removes an object from S3.
	DeleteObject(ctx context.Context, bucket, key string) error

	// ObjectExists checks if an object exists in S3.
	ObjectExists(ctx context.Context, bucket, key string) (bool, error)

	// ObjectSize returns the size in bytes of the object without downloading
	// it. Implementations should use a HEAD-style call (e.g. S3 HeadObject).
	ObjectSize(ctx context.Context, bucket, key string) (int64, error)

	// CopyObject performs a server-side copy from srcKey to destKey within
	// the same bucket. Bytes do not transit the client. Used by S3Cache's
	// staged-write commit step to promote a staging object to its final
	// content-addressed key without re-uploading the data.
	CopyObject(ctx context.Context, bucket, srcKey, destKey string) error
}

// AWSS3Adapter adapts the AWS S3 client to the S3Client interface.
type AWSS3Adapter struct {
	client   *s3.Client
	uploader *manager.Uploader
}

// NewAWSS3Adapter creates a new adapter for the AWS S3 client.
// The adapter wraps the provided S3 client with a streaming multipart Uploader
// so PutObject can stream arbitrarily large bodies without buffering them in memory.
func NewAWSS3Adapter(client *s3.Client) *AWSS3Adapter {
	uploader := manager.NewUploader(client, func(u *manager.Uploader) {
		// 16 MiB parts; AWS minimum is 5 MiB. Larger parts mean fewer requests
		// for big layers but a bigger floor on per-upload memory usage.
		u.PartSize = 16 * 1024 * 1024
		// Concurrency per upload — keep this modest because grog already runs
		// many uploads in parallel at the target/output level.
		u.Concurrency = 4
	})
	return &AWSS3Adapter{
		client:   client,
		uploader: uploader,
	}
}

// GetObject retrieves an object from S3 using the AWS S3 client.
func (a *AWSS3Adapter) GetObject(ctx context.Context, bucket, key string) (io.ReadCloser, error) {
	input := &s3.GetObjectInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(key),
	}

	result, err := a.client.GetObject(ctx, input)
	if err != nil {
		return nil, err
	}

	return result.Body, nil
}

// PutObject uploads an object to S3 by streaming the body through the
// multipart Uploader. The body is consumed lazily — at most PartSize *
// Concurrency bytes are buffered in memory at a time, regardless of total size.
func (a *AWSS3Adapter) PutObject(ctx context.Context, bucket, key string, body io.Reader) error {
	_, err := a.uploader.Upload(ctx, &s3.PutObjectInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(key),
		Body:   body,
	})
	if err != nil {
		return fmt.Errorf("s3 upload: %w", err)
	}
	return nil
}

// DeleteObject removes an object from S3 using the AWS S3 client.
func (a *AWSS3Adapter) DeleteObject(ctx context.Context, bucket, key string) error {
	input := &s3.DeleteObjectInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(key),
	}

	_, err := a.client.DeleteObject(ctx, input)
	return err
}

// CopyObject performs a server-side copy. Bytes never transit the client.
//
// Limitation: S3 single-call CopyObject is capped at 5 GiB. Docker layers
// larger than that are rare in practice but not impossible — when one is
// pushed through this backend the daemon will see a 4xx from the StagedWriter
// commit step and surface it as a push failure with a clear "InvalidRequest"
// error message from the AWS SDK. The fix is to fall back to multipart copy
// (CreateMultipartUpload + UploadPartCopy + CompleteMultipartUpload) when the
// source object exceeds the limit; tracked as a follow-up rather than added
// here to keep this PR focused on the streaming refactor that landed the
// staged-writer interface.
func (a *AWSS3Adapter) CopyObject(ctx context.Context, bucket, srcKey, destKey string) error {
	_, err := a.client.CopyObject(ctx, &s3.CopyObjectInput{
		Bucket:     aws.String(bucket),
		Key:        aws.String(destKey),
		CopySource: aws.String(bucket + "/" + srcKey),
	})
	if err != nil {
		return fmt.Errorf("s3 copy %s -> %s: %w", srcKey, destKey, err)
	}
	return nil
}

// ObjectSize returns the size of an object in S3 via a HeadObject call —
// no body is downloaded.
func (a *AWSS3Adapter) ObjectSize(ctx context.Context, bucket, key string) (int64, error) {
	out, err := a.client.HeadObject(ctx, &s3.HeadObjectInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		return 0, err
	}
	if out.ContentLength == nil {
		return 0, fmt.Errorf("s3 head: missing content length for %s/%s", bucket, key)
	}
	return *out.ContentLength, nil
}

// ObjectExists checks if an object exists in S3 using the AWS S3 client.
func (a *AWSS3Adapter) ObjectExists(ctx context.Context, bucket, key string) (bool, error) {
	input := &s3.HeadObjectInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(key),
	}

	_, err := a.client.HeadObject(ctx, input)
	if err != nil {
		var nsk *types.NoSuchKey
		if errors.As(err, &nsk) {
			return false, nil
		}
		var nsb *types.NotFound
		if errors.As(err, &nsb) {
			return false, nil
		}
		return false, err
	}

	return true, nil
}

// S3Cache implements the CacheBackend interface using AWS S3.
type S3Cache struct {
	bucketName      string
	prefix          string
	workspacePrefix string
	client          S3Client
	logger          *console.Logger
}

func (s *S3Cache) TypeName() string {
	return "s3"
}

// NewS3Cache creates a new S3 cache.
func NewS3Cache(
	ctx context.Context,
	cacheConfig grogconfig.S3CacheConfig,
) (*S3Cache, error) {
	if cacheConfig.Bucket == "" {
		return nil, fmt.Errorf("s3 bucket name is not set")
	}

	// Load the AWS SDK configuration
	cfg, err := config.LoadDefaultConfig(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to load AWS configuration: %w", err)
	}

	// Create an S3 client
	s3Client := s3.NewFromConfig(cfg)
	adapter := NewAWSS3Adapter(s3Client)

	return NewS3CacheWithClient(ctx, cacheConfig, adapter)
}

// NewS3CacheWithClient creates a new S3 cache with a provided client.
// This is useful for testing with a mock client.
func NewS3CacheWithClient(
	ctx context.Context,
	cacheConfig grogconfig.S3CacheConfig,
	client S3Client,
) (*S3Cache, error) {
	if cacheConfig.Bucket == "" {
		return nil, fmt.Errorf("s3 bucket name is not set")
	}

	prefix := cacheConfig.Prefix
	if prefix == "" {
		prefix = ""
	} else {
		prefix = strings.Trim(prefix, "/")
	}

	workspaceDir := grogconfig.Global.WorkspaceRoot
	var workspacePrefix string

	if cacheConfig.SharedCache {
		// If shared cache is enabled treat prefix as the workspace root
		workspacePrefix = ""
	} else {
		// If shared cache is disabled, use the full path hash
		workspacePrefix = strings.Trim(grogconfig.GetWorkspaceCachePrefix(workspaceDir), "/")
	}

	logger := console.GetLogger(ctx)
	logger.Tracef("Instantiated s3 cache at bucket %s with prefix %s and workspace dir %s",
		cacheConfig.Bucket,
		prefix,
		workspacePrefix)
	return &S3Cache{
		logger:          logger,
		client:          client,
		bucketName:      cacheConfig.Bucket,
		prefix:          prefix,
		workspacePrefix: workspacePrefix,
	}, nil
}

func (s *S3Cache) fullPrefix() string {
	if s.prefix == "" {
		return s.workspacePrefix
	}
	if s.workspacePrefix == "" {
		return s.prefix
	}
	return s.prefix + "/" + s.workspacePrefix
}

// buildPath constructs the full S3 path for a cached item.
func (s *S3Cache) buildPath(path, key string) string {
	return joinObjectPath(s.fullPrefix(), path, physicalCacheKey(key))
}

// Get retrieves a cached file from S3.
func (s *S3Cache) Get(ctx context.Context, path, key string) (io.ReadCloser, error) {
	logger := console.GetLogger(ctx)
	s3Path := s.buildPath(path, key)
	logger.Tracef("Getting file from s3 for path: %s", s3Path)

	return s.client.GetObject(ctx, s.bucketName, s3Path)
}

// Set stores a file in S3.
func (s *S3Cache) Set(ctx context.Context, path, key string, content io.Reader) error {
	logger := console.GetLogger(ctx)
	s3Path := s.buildPath(path, key)
	logger.Tracef("Setting file in s3 for path: %s", s3Path)

	return s.client.PutObject(ctx, s.bucketName, s3Path, content)
}

// Delete removes a cached file from S3.
func (s *S3Cache) Delete(ctx context.Context, path string, key string) error {
	logger := console.GetLogger(ctx)
	s3Path := s.buildPath(path, key)
	logger.Tracef("Deleting file from s3 for path: %s", s3Path)

	return s.client.DeleteObject(ctx, s.bucketName, s3Path)
}

// s3StagingPath is the path component used for staged uploads under the
// configured workspace prefix. Anything under this prefix is in-flight and
// must be invisible to Get/Exists/Size against any cache key.
const s3StagingPath = ".uploads"

// BeginWrite opens a streaming upload to a staging key under .uploads/<uuid>.
// On Commit, the staging object is server-side copied to its final
// content-addressed key and the staging object is deleted. No bytes pass
// through grog twice.
func (s *S3Cache) BeginWrite(ctx context.Context) (StagedWriter, error) {
	stagingKey := s.buildPath(s3StagingPath, uuid.NewString())

	pr, pw := io.Pipe()
	sw := &s3StagedWriter{
		client:     s.client,
		bucket:     s.bucketName,
		stagingKey: stagingKey,
		pipeWriter: pw,
		done:       make(chan error, 1),
		buildPath:  s.buildPath,
	}

	go func() {
		// PutObject streams from the pipe via s3manager.Uploader. The Upload
		// call only returns once the pipe sees EOF (success) or an error.
		err := s.client.PutObject(ctx, s.bucketName, stagingKey, pr)
		_ = pr.CloseWithError(err) // unblock any pending writers if upload errored mid-stream
		sw.done <- err
	}()

	return sw, nil
}

// s3StagedWriter is the StagedWriter implementation for S3. Writes go through
// an io.Pipe into a background s3manager.Uploader; Commit is a server-side
// CopyObject from the staging key to the final cache key.
type s3StagedWriter struct {
	client     S3Client
	bucket     string
	stagingKey string
	pipeWriter *io.PipeWriter
	done       chan error
	buildPath  func(path, key string) string

	mu       sync.Mutex
	finished bool
}

func (w *s3StagedWriter) Write(p []byte) (int, error) {
	return w.pipeWriter.Write(p)
}

func (w *s3StagedWriter) Commit(ctx context.Context, path, key string) error {
	w.mu.Lock()
	if w.finished {
		w.mu.Unlock()
		return errors.New("s3 staged writer: commit after commit/cancel")
	}
	w.finished = true
	w.mu.Unlock()

	// Closing the pipe writer with EOF lets the background upload finish
	// successfully. Wait for the upload to drain before issuing the copy.
	if err := w.pipeWriter.Close(); err != nil {
		_ = w.cleanupStaging(ctx)
		return fmt.Errorf("close staging pipe: %w", err)
	}
	if err := <-w.done; err != nil {
		_ = w.cleanupStaging(ctx)
		return fmt.Errorf("upload to staging key: %w", err)
	}

	finalKey := w.buildPath(path, key)
	if err := w.client.CopyObject(ctx, w.bucket, w.stagingKey, finalKey); err != nil {
		_ = w.cleanupStaging(ctx)
		return fmt.Errorf("copy staging -> final: %w", err)
	}

	// Best-effort cleanup of the staging object — the final object exists
	// regardless of whether this delete succeeds.
	_ = w.cleanupStaging(ctx)
	return nil
}

func (w *s3StagedWriter) Cancel(ctx context.Context) error {
	w.mu.Lock()
	if w.finished {
		w.mu.Unlock()
		return nil
	}
	w.finished = true
	w.mu.Unlock()

	// Close the pipe with an error so the upload goroutine sees a failed
	// read, the s3manager.Uploader aborts the multipart upload internally,
	// and we can drain the done channel without deadlocking.
	_ = w.pipeWriter.CloseWithError(errors.New("s3 staged write cancelled"))
	<-w.done

	return w.cleanupStaging(ctx)
}

func (w *s3StagedWriter) cleanupStaging(ctx context.Context) error {
	if err := w.client.DeleteObject(ctx, w.bucket, w.stagingKey); err != nil {
		// Not fatal — orphan staging objects can be cleaned up by an S3
		// lifecycle rule on the .uploads/ prefix. We log via the caller.
		return err
	}
	return nil
}

// Size returns the byte size of an object in S3 via the underlying client's
// HeadObject-equivalent call. No body is downloaded.
func (s *S3Cache) Size(ctx context.Context, path, key string) (int64, error) {
	logger := console.GetLogger(ctx)
	s3Path := s.buildPath(path, key)
	logger.Tracef("Sizing object in s3 for path: %s", s3Path)

	return s.client.ObjectSize(ctx, s.bucketName, s3Path)
}

// Exists checks if a file exists in S3.
func (s *S3Cache) Exists(ctx context.Context, path string, key string) (bool, error) {
	logger := console.GetLogger(ctx)
	s3Path := s.buildPath(path, key)
	logger.Tracef("Checking existence of file in s3 for path: %s", s3Path)

	return s.client.ObjectExists(ctx, s.bucketName, s3Path)
}

// ListKeys uses S3 ListObjectsV2 to list keys under the given path.
func (sc *S3Cache) ListKeys(ctx context.Context, path string, suffix string) ([]string, error) {
	adapter, ok := sc.client.(*AWSS3Adapter)
	if !ok {
		return nil, nil
	}

	fullPath := sc.buildPath(path, "")
	if !strings.HasSuffix(fullPath, "/") {
		fullPath += "/"
	}

	var keys []string
	paginator := s3.NewListObjectsV2Paginator(adapter.client, &s3.ListObjectsV2Input{
		Bucket: aws.String(sc.bucketName),
		Prefix: aws.String(fullPath),
	})

	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			return nil, err
		}
		for _, obj := range page.Contents {
			physicalKey := strings.TrimPrefix(*obj.Key, fullPath)
			logicalKey, decodeErr := decodeCacheKey(physicalKey)
			if decodeErr != nil {
				return nil, decodeErr
			}
			if suffix == "" || strings.HasSuffix(logicalKey, suffix) {
				keys = append(keys, logicalKey)
			}
		}
	}
	return keys, nil
}
