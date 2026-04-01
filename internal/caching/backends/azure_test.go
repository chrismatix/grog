package backends

import (
	"bytes"
	"context"
	"errors"
	"io"
	"testing"

	"github.com/stretchr/testify/assert"

	"grog/internal/config"
)

// mockAzureBlobClient implements the AzureBlobClient interface for testing.
type mockAzureBlobClient struct {
	objects         map[string][]byte
	getBlobCalls    int
	uploadBlobCalls int
	deleteBlobCalls int
	blobExistsCalls int
}

func newMockAzureBlobClient() *mockAzureBlobClient {
	return &mockAzureBlobClient{
		objects: make(map[string][]byte),
	}
}

func (m *mockAzureBlobClient) GetBlob(ctx context.Context, container, blob string) (io.ReadCloser, error) {
	m.getBlobCalls++
	data, ok := m.objects[blob]
	if !ok {
		return nil, errors.New("blob not found")
	}
	return io.NopCloser(bytes.NewReader(data)), nil
}

func (m *mockAzureBlobClient) UploadBlob(ctx context.Context, container, blob string, body io.Reader) error {
	m.uploadBlobCalls++
	data, err := io.ReadAll(body)
	if err != nil {
		return err
	}
	m.objects[blob] = data
	return nil
}

func (m *mockAzureBlobClient) DeleteBlob(ctx context.Context, container, blob string) error {
	m.deleteBlobCalls++
	if _, ok := m.objects[blob]; !ok {
		return errors.New("blob not found")
	}
	delete(m.objects, blob)
	return nil
}

func (m *mockAzureBlobClient) BlobExists(ctx context.Context, container, blob string) (bool, error) {
	m.blobExistsCalls++
	_, ok := m.objects[blob]
	return ok, nil
}

func TestAzureCache_TypeName(t *testing.T) {
	cache := &AzureCache{}
	assert.Equal(t, "azure", cache.TypeName())
}

func TestAzureCache_Get(t *testing.T) {
	ctx := context.Background()
	mockClient := newMockAzureBlobClient()

	// Set up test data
	testData := []byte("test data")
	mockClient.objects["prefix/workspace/path/key"] = testData

	cache, err := NewAzureCacheWithClient(ctx, config.AzureCacheConfig{
		Container: "test-container",
		Prefix:    "prefix",
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

func TestAzureCache_Set(t *testing.T) {
	ctx := context.Background()
	mockClient := newMockAzureBlobClient()

	cache, err := NewAzureCacheWithClient(ctx, config.AzureCacheConfig{
		Container: "test-container",
		Prefix:    "prefix",
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

func TestAzureCache_Delete(t *testing.T) {
	ctx := context.Background()
	mockClient := newMockAzureBlobClient()

	// Set up test data
	mockClient.objects["prefix/workspace/path/key"] = []byte("test data")

	cache, err := NewAzureCacheWithClient(ctx, config.AzureCacheConfig{
		Container: "test-container",
		Prefix:    "prefix",
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

func TestAzureCache_Exists(t *testing.T) {
	ctx := context.Background()
	mockClient := newMockAzureBlobClient()

	// Set up test data
	mockClient.objects["prefix/workspace/path/key"] = []byte("test data")

	cache, err := NewAzureCacheWithClient(ctx, config.AzureCacheConfig{
		Container: "test-container",
		Prefix:    "prefix",
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

func TestAzureCache_MethodCalls(t *testing.T) {
	ctx := context.Background()
	mockClient := newMockAzureBlobClient()

	// Set up test data
	testData := []byte("test data")
	mockClient.objects["prefix/workspace/path/key"] = testData

	cache, err := NewAzureCacheWithClient(ctx, config.AzureCacheConfig{
		Container: "test-container",
		Prefix:    "prefix",
	}, mockClient)
	assert.NoError(t, err)

	// Override workspacePrefix for testing
	cache.workspacePrefix = "workspace"

	// Test all methods to ensure they call the appropriate AzureBlobClient methods

	// Test Get
	_, err = cache.Get(ctx, "path", "key")
	assert.NoError(t, err)
	assert.Equal(t, 1, mockClient.getBlobCalls, "Get should call GetBlob once")

	// Test Set
	err = cache.Set(ctx, "path", "key2", bytes.NewReader(testData))
	assert.NoError(t, err)
	assert.Equal(t, 1, mockClient.uploadBlobCalls, "Set should call UploadBlob once")

	// Test Delete
	err = cache.Delete(ctx, "path", "key")
	assert.NoError(t, err)
	assert.Equal(t, 1, mockClient.deleteBlobCalls, "Delete should call DeleteBlob once")

	// Test Exists
	_, err = cache.Exists(ctx, "path", "key2")
	assert.NoError(t, err)
	assert.Equal(t, 1, mockClient.blobExistsCalls, "Exists should call BlobExists once")
}

func TestAzureCache_SharedCache(t *testing.T) {
	ctx := context.Background()
	mockClient := newMockAzureBlobClient()

	t.Run("shared cache enabled", func(t *testing.T) {
		cache, err := NewAzureCacheWithClient(ctx, config.AzureCacheConfig{
			Container:   "test-container",
			Prefix:      "prefix",
			SharedCache: true,
		}, mockClient)
		assert.NoError(t, err)
		assert.Equal(t, "", cache.workspacePrefix)
		assert.Equal(t, "prefix/path/key", cache.buildPath("path", "key"))
	})

	t.Run("shared cache disabled", func(t *testing.T) {
		cache, err := NewAzureCacheWithClient(ctx, config.AzureCacheConfig{
			Container:   "test-container",
			Prefix:      "prefix",
			SharedCache: false,
		}, mockClient)
		assert.NoError(t, err)
		assert.NotEqual(t, "", cache.workspacePrefix)
		assert.Contains(t, cache.buildPath("path", "key"), cache.workspacePrefix)
	})
}

func TestAzureCache_EmptyContainer(t *testing.T) {
	ctx := context.Background()
	mockClient := newMockAzureBlobClient()

	_, err := NewAzureCacheWithClient(ctx, config.AzureCacheConfig{
		Container: "",
		Prefix:    "prefix",
	}, mockClient)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "azure container name is not set")
}

func TestAzureCache_BuildPath(t *testing.T) {
	ctx := context.Background()
	mockClient := newMockAzureBlobClient()

	t.Run("with prefix and workspace prefix", func(t *testing.T) {
		cache, err := NewAzureCacheWithClient(ctx, config.AzureCacheConfig{
			Container:   "test-container",
			Prefix:      "prefix",
			SharedCache: true,
		}, mockClient)
		assert.NoError(t, err)
		cache.workspacePrefix = "workspace"
		assert.Equal(t, "prefix/workspace/path/key", cache.buildPath("path", "key"))
	})

	t.Run("with prefix only", func(t *testing.T) {
		cache, err := NewAzureCacheWithClient(ctx, config.AzureCacheConfig{
			Container:   "test-container",
			Prefix:      "prefix",
			SharedCache: true,
		}, mockClient)
		assert.NoError(t, err)
		assert.Equal(t, "prefix/path/key", cache.buildPath("path", "key"))
	})

	t.Run("with workspace prefix only", func(t *testing.T) {
		cache, err := NewAzureCacheWithClient(ctx, config.AzureCacheConfig{
			Container:   "test-container",
			SharedCache: true,
		}, mockClient)
		assert.NoError(t, err)
		cache.workspacePrefix = "workspace"
		assert.Equal(t, "workspace/path/key", cache.buildPath("path", "key"))
	})

	t.Run("prefix trimming", func(t *testing.T) {
		cache, err := NewAzureCacheWithClient(ctx, config.AzureCacheConfig{
			Container:   "test-container",
			Prefix:      "/prefix/",
			SharedCache: true,
		}, mockClient)
		assert.NoError(t, err)
		assert.Equal(t, "prefix/path/key", cache.buildPath("path", "key"))
	})
}
