package backends

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/chrismatix/grog/internal/config"
)

// fakeAzureServer serves the subset of the Azure Blob REST API that AzureBlobAdapter uses
// for a single account named "account".
type fakeAzureServer struct {
	mutex        sync.Mutex
	objects      map[string][]byte
	copyStatus   string
	failRequests bool
}

func (server *fakeAzureServer) ServeHTTP(writer http.ResponseWriter, request *http.Request) {
	server.mutex.Lock()
	defer server.mutex.Unlock()

	if server.failRequests {
		writer.Header().Set("x-ms-error-code", "InternalError")
		writer.WriteHeader(http.StatusBadRequest)
		return
	}

	containerAndBlob := strings.SplitN(strings.TrimPrefix(request.URL.Path, "/account/"), "/", 2)
	query := request.URL.Query()
	if len(containerAndBlob) == 1 && query.Get("comp") == "list" {
		server.writeBlobList(writer, query.Get("prefix"))
		return
	}
	blobName := containerAndBlob[1]

	switch request.Method {
	case http.MethodPut:
		switch {
		case request.Header.Get("x-ms-copy-source") != "":
			sourceURL, _ := url.Parse(request.Header.Get("x-ms-copy-source"))
			sourceName := strings.SplitN(strings.TrimPrefix(sourceURL.Path, "/account/"), "/", 2)[1]
			server.objects[blobName] = append([]byte(nil), server.objects[sourceName]...)
			writer.Header().Set("x-ms-copy-status", "success")
			writer.WriteHeader(http.StatusAccepted)
		default:
			content, _ := io.ReadAll(request.Body)
			server.objects[blobName] = content
			writer.WriteHeader(http.StatusCreated)
		}
	case http.MethodDelete:
		if _, found := server.objects[blobName]; !found {
			writer.Header().Set("x-ms-error-code", "BlobNotFound")
			writer.WriteHeader(http.StatusNotFound)
			return
		}
		delete(server.objects, blobName)
		writer.WriteHeader(http.StatusAccepted)
	case http.MethodHead, http.MethodGet:
		content, found := server.objects[blobName]
		if !found {
			writer.Header().Set("x-ms-error-code", "BlobNotFound")
			writer.WriteHeader(http.StatusNotFound)
			return
		}
		writer.Header().Set("Content-Length", strconv.Itoa(len(content)))
		if server.copyStatus != "" {
			writer.Header().Set("x-ms-copy-status", server.copyStatus)
		}
		if request.Method == http.MethodGet {
			_, _ = writer.Write(content)
		}
	}
}

func (server *fakeAzureServer) writeBlobList(writer http.ResponseWriter, prefix string) {
	var names []string
	for name := range server.objects {
		if strings.HasPrefix(name, prefix) {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	var body strings.Builder
	body.WriteString(`<?xml version="1.0" encoding="utf-8"?><EnumerationResults><Blobs>`)
	for _, name := range names {
		body.WriteString("<Blob><Name>" + name + "</Name><Properties><Content-Length>" +
			strconv.Itoa(len(server.objects[name])) + "</Content-Length></Properties></Blob>")
	}
	body.WriteString("</Blobs><NextMarker /></EnumerationResults>")
	writer.Header().Set("Content-Type", "application/xml")
	_, _ = io.WriteString(writer, body.String())
}

func newTestAzureCache(t *testing.T) (*AzureCache, *fakeAzureServer) {
	t.Helper()
	fakeServer := &fakeAzureServer{objects: map[string][]byte{}, copyStatus: "success"}
	httpServer := httptest.NewServer(fakeServer)
	t.Cleanup(httpServer.Close)

	cache, err := NewAzureCache(context.Background(), config.AzureCacheConfig{
		ConnectionString: "DefaultEndpointsProtocol=http;AccountName=account;AccountKey=a2V5;BlobEndpoint=" + httpServer.URL + "/account;",
		Container:        "container",
		Prefix:           "prefix",
		SharedCache:      true,
	})
	require.NoError(t, err)
	return cache, fakeServer
}

func TestNewAzureCache_ConfigErrors(t *testing.T) {
	testCases := []struct {
		name          string
		cacheConfig   config.AzureCacheConfig
		expectedError string
	}{
		{
			name:          "missing container",
			cacheConfig:   config.AzureCacheConfig{},
			expectedError: "azure container name is not set",
		},
		{
			name:          "missing credentials",
			cacheConfig:   config.AzureCacheConfig{Container: "container"},
			expectedError: "requires either account_url or connection_string",
		},
		{
			name:          "invalid connection string",
			cacheConfig:   config.AzureCacheConfig{Container: "container", ConnectionString: "invalid"},
			expectedError: "failed to create Azure client from connection string",
		},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			_, err := NewAzureCache(context.Background(), testCase.cacheConfig)
			assert.ErrorContains(t, err, testCase.expectedError)
		})
	}
}

func TestNewAzureCache_AccountURL(t *testing.T) {
	cache, err := NewAzureCache(context.Background(), config.AzureCacheConfig{
		AccountURL: "https://account.blob.core.windows.net",
		Container:  "container",
	})
	require.NoError(t, err)
	assert.IsType(t, &AzureBlobAdapter{}, cache.client)
}

func TestAzureBlobAdapter_RoundTrip(t *testing.T) {
	ctx := context.Background()
	cache, _ := newTestAzureCache(t)

	exists, err := cache.Exists(ctx, "path", "key")
	require.NoError(t, err)
	assert.False(t, exists)
	_, err = cache.Get(ctx, "path", "key")
	assert.Error(t, err)
	_, err = cache.Size(ctx, "path", "key")
	assert.Error(t, err)

	require.NoError(t, cache.Set(ctx, "path", "key", strings.NewReader("content")))

	exists, err = cache.Exists(ctx, "path", "key")
	require.NoError(t, err)
	assert.True(t, exists)

	size, err := cache.Size(ctx, "path", "key")
	require.NoError(t, err)
	assert.Equal(t, int64(len("content")), size)

	reader, err := cache.Get(ctx, "path", "key")
	require.NoError(t, err)
	content, err := io.ReadAll(reader)
	require.NoError(t, err)
	require.NoError(t, reader.Close())
	assert.Equal(t, "content", string(content))

	require.NoError(t, cache.Delete(ctx, "path", "key"))
	exists, err = cache.Exists(ctx, "path", "key")
	require.NoError(t, err)
	assert.False(t, exists)
}

func TestAzureBlobAdapter_ExistsPropagatesServerError(t *testing.T) {
	cache, fakeServer := newTestAzureCache(t)
	fakeServer.failRequests = true
	_, err := cache.Exists(context.Background(), "path", "key")
	assert.Error(t, err)
}

func TestAzureBlobAdapter_StagedWriteAndListKeys(t *testing.T) {
	ctx := context.Background()
	cache, fakeServer := newTestAzureCache(t)

	stagedWriter, err := cache.BeginWrite(ctx)
	require.NoError(t, err)
	_, err = stagedWriter.Write([]byte("staged content"))
	require.NoError(t, err)
	require.NoError(t, stagedWriter.Commit(ctx, "traces", "day/a.parquet"))
	require.NoError(t, cache.Set(ctx, "traces", "day/b.txt", strings.NewReader("b")))

	assert.Equal(t, "staged content", string(fakeServer.objects["prefix/traces/day/a.parquet"]))
	assert.Len(t, fakeServer.objects, 2)

	testCases := []struct {
		name         string
		suffix       string
		expectedKeys []string
	}{
		{name: "no suffix", suffix: "", expectedKeys: []string{"day/a.parquet", "day/b.txt"}},
		{name: "parquet suffix", suffix: ".parquet", expectedKeys: []string{"day/a.parquet"}},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			keys, err := cache.ListKeys(ctx, "traces", testCase.suffix)
			require.NoError(t, err)
			assert.Equal(t, testCase.expectedKeys, keys)
		})
	}
}

func TestAzureBlobAdapter_CopyBlobFailureStatuses(t *testing.T) {
	testCases := []struct {
		name          string
		copyStatus    string
		expectedError string
	}{
		{name: "failed", copyStatus: "failed", expectedError: "failed"},
		{name: "aborted", copyStatus: "aborted", expectedError: "aborted"},
		{name: "unexpected", copyStatus: "bogus", expectedError: "unexpected status"},
		{name: "missing", copyStatus: "", expectedError: "missing copy status"},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			ctx := context.Background()
			cache, fakeServer := newTestAzureCache(t)
			fakeServer.copyStatus = testCase.copyStatus
			fakeServer.objects["source"] = []byte("content")

			adapter := cache.client.(*AzureBlobAdapter)
			err := adapter.CopyBlob(ctx, "container", "source", "destination")
			assert.ErrorContains(t, err, testCase.expectedError)
		})
	}
}

func TestAzureCache_ListKeysWithoutAdapter(t *testing.T) {
	cache, err := NewAzureCacheWithClient(context.Background(), config.AzureCacheConfig{Container: "container"}, newMockAzureBlobClient())
	require.NoError(t, err)
	keys, err := cache.ListKeys(context.Background(), "traces", "")
	assert.NoError(t, err)
	assert.Nil(t, keys)
}
