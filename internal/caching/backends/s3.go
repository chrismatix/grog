package backends

import (
	"context"
	"fmt"
	"go.uber.org/zap"
	grogconfig "grog/internal/config"
	"grog/internal/console"
	"io"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

// s3ClientInterface defines the interface for S3 client operations
type s3ClientInterface interface {
	GetObject(ctx context.Context, params *s3.GetObjectInput, optFns ...func(*s3.Options)) (*s3.GetObjectOutput, error)
	PutObject(ctx context.Context, params *s3.PutObjectInput, optFns ...func(*s3.Options)) (*s3.PutObjectOutput, error)
	DeleteObject(ctx context.Context, params *s3.DeleteObjectInput, optFns ...func(*s3.Options)) (*s3.DeleteObjectOutput, error)
	HeadObject(ctx context.Context, params *s3.HeadObjectInput, optFns ...func(*s3.Options)) (*s3.HeadObjectOutput, error)
}

// Ensure that the real S3 client implements our interface
var _ s3ClientInterface = (*s3.Client)(nil)

// S3Cache implements the CacheBackend interface using Amazon S3.
type S3Cache struct {
	bucketName      string
	prefix          string
	workspacePrefix string
	client          s3ClientInterface
	logger          *zap.SugaredLogger
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
		return nil, fmt.Errorf("S3 bucket name is not set")
	}

	// Load AWS configuration
	cfg, err := awsconfig.LoadDefaultConfig(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to load AWS config: %w", err)
	}

	// Create S3 client
	client := s3.NewFromConfig(cfg)

	prefix := cacheConfig.Prefix
	if prefix == "" {
		prefix = ""
	} else {
		prefix = strings.Trim(prefix, "/")
	}

	workspaceDir := grogconfig.Global.WorkspaceRoot
	workspacePrefix := strings.Trim(grogconfig.GetWorkspaceCachePrefix(workspaceDir), "/")

	logger := console.GetLogger(ctx)
	logger.Debugf("Instantiated S3 cache at bucket %s with prefix %s and workspace dir %s",
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
	logger.Debugf("Getting file from S3 for path: %s", s3Path)

	output, err := s.client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(s.bucketName),
		Key:    aws.String(s3Path),
	})
	if err != nil {
		logger.Debugf("Failed to get file from S3 for path: %s, key: %s: %v", path, key, err)
		return nil, fmt.Errorf("failed to get object: %w", err)
	}

	return output.Body, nil
}

// Set stores a file in S3.
func (s *S3Cache) Set(ctx context.Context, path, key string, content io.Reader) error {
	logger := console.GetLogger(ctx)
	s3Path := s.buildPath(path, key)
	logger.Debugf("Setting file in S3 for path: %s", s3Path)

	_, err := s.client.PutObject(ctx, &s3.PutObjectInput{
		Bucket: aws.String(s.bucketName),
		Key:    aws.String(s3Path),
		Body:   content,
	})
	if err != nil {
		return fmt.Errorf("failed to put object: %w", err)
	}

	return nil
}

// Delete removes a cached file from S3.
func (s *S3Cache) Delete(ctx context.Context, path string, key string) error {
	logger := console.GetLogger(ctx)
	s3Path := s.buildPath(path, key)
	logger.Debugf("Deleting file from S3 for path: %s", s3Path)

	_, err := s.client.DeleteObject(ctx, &s3.DeleteObjectInput{
		Bucket: aws.String(s.bucketName),
		Key:    aws.String(s3Path),
	})
	if err != nil {
		return fmt.Errorf("failed to delete object: %w", err)
	}

	return nil
}

// Exists checks if a file exists in S3.
func (s *S3Cache) Exists(ctx context.Context, path string, key string) (bool, error) {
	logger := console.GetLogger(ctx)
	s3Path := s.buildPath(path, key)
	logger.Debugf("Checking existence of file in S3 for path: %s", s3Path)

	_, err := s.client.HeadObject(ctx, &s3.HeadObjectInput{
		Bucket: aws.String(s.bucketName),
		Key:    aws.String(s3Path),
	})
	if err != nil {
		// In AWS SDK v2, we can check for specific error types
		logger.Debugf("File does not exist: %s", s3Path)
		return false, nil
	}

	return true, nil
}

// Clear is not supported for remote caches
func (s *S3Cache) Clear(ctx context.Context, expunge bool) error {
	return nil
}
