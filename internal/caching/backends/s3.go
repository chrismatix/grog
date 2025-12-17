package backends

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"

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
}

// AWSS3Adapter adapts the AWS S3 client to the S3Client interface.
type AWSS3Adapter struct {
	client *s3.Client
}

// NewAWSS3Adapter creates a new adapter for the AWS S3 client.
func NewAWSS3Adapter(client *s3.Client) *AWSS3Adapter {
	return &AWSS3Adapter{
		client: client,
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

// PutObject uploads an object to S3 using the AWS S3 client.
func (a *AWSS3Adapter) PutObject(ctx context.Context, bucket, key string, body io.Reader) error {
	data, err := io.ReadAll(body)
	if err != nil {
		return fmt.Errorf("read body: %w", err)
	}

	input := &s3.PutObjectInput{
		Bucket:        aws.String(bucket),
		Key:           aws.String(key),
		Body:          bytes.NewReader(data),
		ContentLength: aws.Int64(int64(len(data))),
	}

	_, err = a.client.PutObject(ctx, input)
	return err
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
	workspacePrefix := strings.Trim(grogconfig.GetWorkspaceCachePrefix(workspaceDir), "/")

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
	return s.prefix + "/" + s.workspacePrefix
}

// buildPath constructs the full S3 path for a cached item.
func (s *S3Cache) buildPath(path, key string) string {
	parts := []string{s.fullPrefix(), strings.Trim(path, "/"), strings.Trim(key, "/")}
	return strings.Join(parts, "/")
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

// Exists checks if a file exists in S3.
func (s *S3Cache) Exists(ctx context.Context, path string, key string) (bool, error) {
	logger := console.GetLogger(ctx)
	s3Path := s.buildPath(path, key)
	logger.Tracef("Checking existence of file in s3 for path: %s", s3Path)

	return s.client.ObjectExists(ctx, s.bucketName, s3Path)
}
