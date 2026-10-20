package backends

import (
	"context"
	"errors"
	"io"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/chrismatix/grog/internal/config"
)

func withTestGlobalConfig(t *testing.T) {
	t.Helper()
	previousGlobal := config.Global
	t.Cleanup(func() { config.Global = previousGlobal })
	config.Global.Root = t.TempDir()
	config.Global.WorkspaceRoot = t.TempDir()
}

func TestGetCacheBackend(t *testing.T) {
	withTestGlobalConfig(t)
	gcsServer := httptest.NewServer(&fakeGCSServer{objects: map[string][]byte{}})
	t.Cleanup(gcsServer.Close)
	t.Setenv("STORAGE_EMULATOR_HOST", gcsServer.URL)
	t.Setenv("AWS_REGION", "us-east-1")
	t.Setenv("AWS_CONFIG_FILE", t.TempDir()+"/missing")
	t.Setenv("AWS_SHARED_CREDENTIALS_FILE", t.TempDir()+"/missing")

	testCases := []struct {
		name             string
		cacheConfig      config.CacheConfig
		expectedTypeName string
		expectedError    string
	}{
		{name: "filesystem", cacheConfig: config.CacheConfig{}, expectedTypeName: "fs"},
		{
			name:             "gcs",
			cacheConfig:      config.CacheConfig{Backend: config.GCSCacheBackend, GCS: config.GCSCacheConfig{Bucket: "bucket"}},
			expectedTypeName: "gcs",
		},
		{
			name:             "s3",
			cacheConfig:      config.CacheConfig{Backend: config.S3CacheBackend, S3: config.S3CacheConfig{Bucket: "bucket"}},
			expectedTypeName: "s3",
		},
		{
			name: "azure",
			cacheConfig: config.CacheConfig{Backend: config.AzureCacheBackend, Azure: config.AzureCacheConfig{
				Container:        "container",
				ConnectionString: "DefaultEndpointsProtocol=http;AccountName=account;AccountKey=a2V5;BlobEndpoint=http://127.0.0.1/account;",
			}},
			expectedTypeName: "azure",
		},
		{name: "gcs without bucket", cacheConfig: config.CacheConfig{Backend: config.GCSCacheBackend}, expectedError: "GCS bucket name is not set"},
		{name: "s3 without bucket", cacheConfig: config.CacheConfig{Backend: config.S3CacheBackend}, expectedError: "s3 bucket name is not set"},
		{name: "azure without container", cacheConfig: config.CacheConfig{Backend: config.AzureCacheBackend}, expectedError: "azure container name is not set"},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			backend, err := GetCacheBackend(context.Background(), testCase.cacheConfig)
			if testCase.expectedError != "" {
				assert.ErrorContains(t, err, testCase.expectedError)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, testCase.expectedTypeName, backend.TypeName())
			assert.IsType(t, &BoundedBackend{}, backend)
		})
	}
}

func TestGetCacheBackend_FileSystemCacheFailsOnUnwritableRoot(t *testing.T) {
	withTestGlobalConfig(t)
	config.Global.Root = "/dev/null/grog"
	_, err := GetCacheBackend(context.Background(), config.CacheConfig{})
	assert.Error(t, err)
}

func TestBoundedBackend_DelegatesToFileSystemCache(t *testing.T) {
	withTestGlobalConfig(t)
	ctx := context.Background()
	backend, err := GetCacheBackend(ctx, config.CacheConfig{})
	require.NoError(t, err)
	boundedBackend := backend.(*BoundedBackend)
	fileSystemCache := boundedBackend.Inner().(*FileSystemCache)

	require.NoError(t, backend.Set(ctx, "traces", "day/a.parquet", strings.NewReader("content")))

	exists, err := backend.Exists(ctx, "traces", "day/a.parquet")
	require.NoError(t, err)
	assert.True(t, exists)

	size, err := backend.Size(ctx, "traces", "day/a.parquet")
	require.NoError(t, err)
	assert.Equal(t, int64(len("content")), size)

	keys, err := backend.ListKeys(ctx, "traces", ".parquet")
	require.NoError(t, err)
	assert.Equal(t, []string{"day/a.parquet"}, keys)

	workspaceCacheSize, err := fileSystemCache.GetWorkspaceCacheSizeBytes()
	require.NoError(t, err)
	assert.Equal(t, int64(len("content")), workspaceCacheSize)
	totalCacheSize, err := fileSystemCache.GetCacheSizeBytes()
	require.NoError(t, err)
	assert.Equal(t, int64(len("content")), totalCacheSize)

	require.NoError(t, backend.Delete(ctx, "traces", "day/a.parquet"))
	_, err = backend.Size(ctx, "traces", "day/a.parquet")
	assert.Error(t, err)

	_, err = backend.Get(ctx, "traces", "day/a.parquet")
	assert.Error(t, err)
}

func TestBoundedBackend_ReturnsContextErrorWhenSaturated(t *testing.T) {
	withGlobalIOConcurrency(t, 1)
	release, err := acquireForBackend(context.Background())
	require.NoError(t, err)
	defer release()

	cancelledContext, cancel := context.WithCancel(context.Background())
	cancel()
	backend := NewBoundedBackend(&fakeBackend{})

	_, err = backend.Get(cancelledContext, "path", "key")
	assert.ErrorIs(t, err, context.Canceled)
	assert.ErrorIs(t, backend.Set(cancelledContext, "path", "key", strings.NewReader("")), context.Canceled)
	assert.ErrorIs(t, backend.Delete(cancelledContext, "path", "key"), context.Canceled)
	_, err = backend.Exists(cancelledContext, "path", "key")
	assert.ErrorIs(t, err, context.Canceled)
	_, err = backend.Size(cancelledContext, "path", "key")
	assert.ErrorIs(t, err, context.Canceled)
	_, err = backend.ListKeys(cancelledContext, "path", "")
	assert.ErrorIs(t, err, context.Canceled)
}

func TestRemoteWrapper_SizeListKeysAndAccessors(t *testing.T) {
	ctx := context.Background()
	fileSystemCache := NewFileSystemCacheForTest(t.TempDir(), t.TempDir())
	remote := &mockCacheBackend{
		typeName: "remote",
		sizeFunc: func(ctx context.Context, path, key string) (int64, error) {
			if key == "remote-only" {
				return 42, nil
			}
			return 0, errors.New("not found")
		},
	}
	remoteWrapper := NewRemoteWrapper(fileSystemCache, remote)

	assert.Equal(t, "remote", remoteWrapper.TypeName())
	assert.Same(t, fileSystemCache, remoteWrapper.GetFS())

	require.NoError(t, fileSystemCache.Set(ctx, "path", "local", strings.NewReader("local")))
	testCases := []struct {
		name          string
		key           string
		expectedSize  int64
		expectedError bool
	}{
		{name: "local hit", key: "local", expectedSize: int64(len("local"))},
		{name: "remote fallback", key: "remote-only", expectedSize: 42},
		{name: "missing everywhere", key: "missing", expectedError: true},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			size, err := remoteWrapper.Size(ctx, "path", testCase.key)
			if testCase.expectedError {
				assert.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, testCase.expectedSize, size)
		})
	}

	keys, err := remoteWrapper.ListKeys(ctx, "path", "")
	assert.NoError(t, err)
	assert.Nil(t, keys)
}

type failingStagedWriter struct {
	commitError error
}

func (writer *failingStagedWriter) Write(content []byte) (int, error) { return len(content), nil }
func (writer *failingStagedWriter) Commit(context.Context, string, string) error {
	return writer.commitError
}
func (writer *failingStagedWriter) Cancel(context.Context) error { return nil }

type stagedWriterBackend struct {
	mockCacheBackend

	stagedWriter StagedWriter
}

func (backend *stagedWriterBackend) BeginWrite(context.Context) (StagedWriter, error) {
	return backend.stagedWriter, nil
}

func TestRemoteWrapper_BeginWriteRemoteCommitFails(t *testing.T) {
	ctx := context.Background()
	fileSystemCache := NewFileSystemCacheForTest(t.TempDir(), t.TempDir())
	remoteStagedWriter := &failingStagedWriter{commitError: errors.New("remote down")}
	remoteWrapper := NewRemoteWrapper(fileSystemCache, &stagedWriterBackend{stagedWriter: remoteStagedWriter})

	stagedWriter, err := remoteWrapper.BeginWrite(ctx)
	require.NoError(t, err)
	_, err = io.WriteString(stagedWriter, "content")
	require.NoError(t, err)

	assert.ErrorContains(t, stagedWriter.Commit(ctx, "cas", "digest"), "remote commit: remote down")
	size, err := fileSystemCache.Size(ctx, "cas", "digest")
	require.NoError(t, err)
	assert.Equal(t, int64(len("content")), size)
	assert.ErrorContains(t, stagedWriter.Commit(ctx, "cas", "digest"), "commit after commit/cancel")
}

func TestRemoteWrapper_BeginWriteRemoteBeginFails(t *testing.T) {
	fileSystemCache := NewFileSystemCacheForTest(t.TempDir(), t.TempDir())
	remoteWrapper := NewRemoteWrapper(fileSystemCache, &mockCacheBackend{})
	_, err := remoteWrapper.BeginWrite(context.Background())
	assert.ErrorContains(t, err, "remote begin write")
}

func TestFileSystemCache_BeginWriteCommitFailsWhenDestinationIsFile(t *testing.T) {
	ctx := context.Background()
	fileSystemCache := NewFileSystemCacheForTest(t.TempDir(), t.TempDir())
	assert.Equal(t, "fs", fileSystemCache.TypeName())
	require.NoError(t, fileSystemCache.Set(ctx, "path", "blocker", strings.NewReader("file")))

	stagedWriter, err := fileSystemCache.BeginWrite(ctx)
	require.NoError(t, err)
	_, err = io.WriteString(stagedWriter, "content")
	require.NoError(t, err)
	assert.ErrorContains(t, stagedWriter.Commit(ctx, "path", "blocker/nested"), "create destination dir")
}
