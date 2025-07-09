package backends

import (
	"bytes"
	"context"
	"errors"
	"grog/internal/config"
	"io"
	"testing"

	"github.com/stretchr/testify/assert"
)

// mockS3Client implements the S3Client interface for testing.
// This mock simulates the behavior of the AWSS3Adapter which adapts the real AWS S3 client.
type mockS3Client struct {
	objects map[string][]byte
	// Add fields to track method calls for verification in tests
	getObjectCalls    int
	putObjectCalls    int
	deleteObjectCalls int
	objectExistsCalls int
}

func newMockS3Client() *mockS3Client {
	return &mockS3Client{
		objects: make(map[string][]byte),
	}
}

func (m *mockS3Client) GetObject(ctx context.Context, bucket, key string) (io.ReadCloser, error) {
	m.getObjectCalls++
	data, ok := m.objects[key]
	if !ok {
		return nil, errors.New("object not found")
	}
	return io.NopCloser(bytes.NewReader(data)), nil
}

func (m *mockS3Client) PutObject(ctx context.Context, bucket, key string, body io.Reader) error {
	m.putObjectCalls++
	data, err := io.ReadAll(body)
	if err != nil {
		return err
	}
	m.objects[key] = data
	return nil
}

func (m *mockS3Client) DeleteObject(ctx context.Context, bucket, key string) error {
	m.deleteObjectCalls++
	if _, ok := m.objects[key]; !ok {
		return errors.New("object not found")
	}
	delete(m.objects, key)
	return nil
}

func (m *mockS3Client) ObjectExists(ctx context.Context, bucket, key string) (bool, error) {
	m.objectExistsCalls++
	_, ok := m.objects[key]
	return ok, nil
}

func TestS3Cache_TypeName(t *testing.T) {
	cache := &S3Cache{}
	assert.Equal(t, "s3", cache.TypeName())
}

func TestS3Cache_Get(t *testing.T) {
	ctx := context.Background()
	mockClient := newMockS3Client()

	// Set up test data
	testData := []byte("test data")
	mockClient.objects["prefix/workspace/path/key"] = testData

	cache, err := NewS3CacheWithClient(ctx, config.S3CacheConfig{
		Bucket: "test-bucket",
		Prefix: "prefix",
	}, mockClient)
	assert.NoError(t, err)

	// Override workspacePrefix for testing
	cache.workspacePrefix = "workspace"

	// Test Get
	reader, err := cache.Get(ctx, "path", "key")
	assert.NoError(t, err)

	data, err := io.ReadAll(reader)
	assert.NoError(t, err)
	assert.Equal(t, testData, data)

	// Test Get with non-existent key
	_, err = cache.Get(ctx, "path", "nonexistent")
	assert.Error(t, err)
}

func TestS3Cache_Set(t *testing.T) {
	ctx := context.Background()
	mockClient := newMockS3Client()

	cache, err := NewS3CacheWithClient(ctx, config.S3CacheConfig{
		Bucket: "test-bucket",
		Prefix: "prefix",
	}, mockClient)
	assert.NoError(t, err)

	// Override workspacePrefix for testing
	cache.workspacePrefix = "workspace"

	// Test Set
	testData := []byte("test data")
	err = cache.Set(ctx, "path", "key", bytes.NewReader(testData))
	assert.NoError(t, err)

	// Verify data was stored correctly
	assert.Equal(t, testData, mockClient.objects["prefix/workspace/path/key"])
}

func TestS3Cache_Delete(t *testing.T) {
	ctx := context.Background()
	mockClient := newMockS3Client()

	// Set up test data
	mockClient.objects["prefix/workspace/path/key"] = []byte("test data")

	cache, err := NewS3CacheWithClient(ctx, config.S3CacheConfig{
		Bucket: "test-bucket",
		Prefix: "prefix",
	}, mockClient)
	assert.NoError(t, err)

	// Override workspacePrefix for testing
	cache.workspacePrefix = "workspace"

	// Test Delete
	err = cache.Delete(ctx, "path", "key")
	assert.NoError(t, err)

	// Verify object was deleted
	_, ok := mockClient.objects["prefix/workspace/path/key"]
	assert.False(t, ok)

	// Test Delete with non-existent key
	err = cache.Delete(ctx, "path", "nonexistent")
	assert.Error(t, err)
}

func TestS3Cache_Exists(t *testing.T) {
	ctx := context.Background()
	mockClient := newMockS3Client()

	// Set up test data
	mockClient.objects["prefix/workspace/path/key"] = []byte("test data")

	cache, err := NewS3CacheWithClient(ctx, config.S3CacheConfig{
		Bucket: "test-bucket",
		Prefix: "prefix",
	}, mockClient)
	assert.NoError(t, err)

	// Override workspacePrefix for testing
	cache.workspacePrefix = "workspace"

	// Test Exists with existing key
	exists, err := cache.Exists(ctx, "path", "key")
	assert.NoError(t, err)
	assert.True(t, exists)

	// Test Exists with non-existent key
	exists, err = cache.Exists(ctx, "path", "nonexistent")
	assert.NoError(t, err)
	assert.False(t, exists)
}

func TestS3Cache_MethodCalls(t *testing.T) {
	ctx := context.Background()
	mockClient := newMockS3Client()

	// Set up test data
	testData := []byte("test data")
	mockClient.objects["prefix/workspace/path/key"] = testData

	cache, err := NewS3CacheWithClient(ctx, config.S3CacheConfig{
		Bucket: "test-bucket",
		Prefix: "prefix",
	}, mockClient)
	assert.NoError(t, err)

	// Override workspacePrefix for testing
	cache.workspacePrefix = "workspace"

	// Test all methods to ensure they call the appropriate S3Client methods

	// Test Get
	_, err = cache.Get(ctx, "path", "key")
	assert.NoError(t, err)
	assert.Equal(t, 1, mockClient.getObjectCalls, "Get should call GetObject once")

	// Test Set
	err = cache.Set(ctx, "path", "key2", bytes.NewReader(testData))
	assert.NoError(t, err)
	assert.Equal(t, 1, mockClient.putObjectCalls, "Set should call PutObject once")

	// Test Delete
	err = cache.Delete(ctx, "path", "key")
	assert.NoError(t, err)
	assert.Equal(t, 1, mockClient.deleteObjectCalls, "Delete should call DeleteObject once")

	// Test Exists
	_, err = cache.Exists(ctx, "path", "key2")
	assert.NoError(t, err)
	assert.Equal(t, 1, mockClient.objectExistsCalls, "Exists should call ObjectExists once")
}
