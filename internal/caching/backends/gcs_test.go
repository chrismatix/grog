package backends

import (
	"context"
	"encoding/json"
	"io"
	"mime"
	"mime/multipart"
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

const testBucketName = "test-bucket"

// fakeGCSServer serves the subset of the GCS JSON and XML APIs that GCSCache uses.
type fakeGCSServer struct {
	mutex   sync.Mutex
	objects map[string][]byte
}

func (server *fakeGCSServer) objectResource(name string) map[string]any {
	return map[string]any{
		"bucket": testBucketName,
		"name":   name,
		"size":   strconv.Itoa(len(server.objects[name])),
	}
}

func writeJSON(writer http.ResponseWriter, status int, body any) {
	writer.Header().Set("Content-Type", "application/json")
	writer.WriteHeader(status)
	_ = json.NewEncoder(writer).Encode(body)
}

func writeNotFound(writer http.ResponseWriter) {
	writeJSON(writer, http.StatusNotFound, map[string]any{
		"error": map[string]any{"code": http.StatusNotFound, "message": "not found"},
	})
}

func (server *fakeGCSServer) ServeHTTP(writer http.ResponseWriter, request *http.Request) {
	server.mutex.Lock()
	defer server.mutex.Unlock()

	segments := strings.Split(strings.TrimPrefix(request.URL.EscapedPath(), "/"), "/")
	for index, segment := range segments {
		segments[index], _ = url.PathUnescape(segment)
	}
	jsonObjectPrefix := []string{"storage", "v1", "b", testBucketName, "o"}
	isJSONObjectPath := len(segments) >= 5 && strings.Join(segments[:5], "/") == strings.Join(jsonObjectPrefix, "/")

	switch {
	case request.Method == http.MethodPost && strings.HasPrefix(request.URL.Path, "/upload/storage/v1/b/"+testBucketName+"/o"):
		server.handleMultipartUpload(writer, request)
	case isJSONObjectPath && len(segments) == 5 && request.Method == http.MethodGet:
		prefix := request.URL.Query().Get("prefix")
		var names []string
		for name := range server.objects {
			if strings.HasPrefix(name, prefix) {
				names = append(names, name)
			}
		}
		sort.Strings(names)
		items := make([]map[string]any, 0, len(names))
		for _, name := range names {
			items = append(items, server.objectResource(name))
		}
		writeJSON(writer, http.StatusOK, map[string]any{"kind": "storage#objects", "items": items})
	case isJSONObjectPath && len(segments) == 11 && segments[6] == "rewriteTo" && request.Method == http.MethodPost:
		sourceName, destinationName := segments[5], segments[10]
		content, found := server.objects[sourceName]
		if !found {
			writeNotFound(writer)
			return
		}
		server.objects[destinationName] = append([]byte(nil), content...)
		writeJSON(writer, http.StatusOK, map[string]any{
			"kind":     "storage#rewriteResponse",
			"done":     true,
			"resource": server.objectResource(destinationName),
		})
	case isJSONObjectPath && len(segments) == 6:
		name := segments[5]
		if _, found := server.objects[name]; !found {
			writeNotFound(writer)
			return
		}
		switch request.Method {
		case http.MethodGet:
			writeJSON(writer, http.StatusOK, server.objectResource(name))
		case http.MethodDelete:
			delete(server.objects, name)
			writer.WriteHeader(http.StatusNoContent)
		}
	case request.Method == http.MethodGet && len(segments) >= 2 && segments[0] == testBucketName:
		content, found := server.objects[strings.Join(segments[1:], "/")]
		if !found {
			writer.WriteHeader(http.StatusNotFound)
			return
		}
		writer.Header().Set("Content-Length", strconv.Itoa(len(content)))
		_, _ = writer.Write(content)
	default:
		http.Error(writer, "unhandled fake GCS request: "+request.Method+" "+request.URL.String(), http.StatusNotImplemented)
	}
}

func (server *fakeGCSServer) handleMultipartUpload(writer http.ResponseWriter, request *http.Request) {
	_, parameters, err := mime.ParseMediaType(request.Header.Get("Content-Type"))
	if err != nil {
		http.Error(writer, err.Error(), http.StatusBadRequest)
		return
	}
	reader := multipart.NewReader(request.Body, parameters["boundary"])
	metadataPart, err := reader.NextPart()
	if err != nil {
		http.Error(writer, err.Error(), http.StatusBadRequest)
		return
	}
	var metadata struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(metadataPart).Decode(&metadata); err != nil {
		http.Error(writer, err.Error(), http.StatusBadRequest)
		return
	}
	mediaPart, err := reader.NextPart()
	if err != nil {
		http.Error(writer, err.Error(), http.StatusBadRequest)
		return
	}
	content, err := io.ReadAll(mediaPart)
	if err != nil {
		http.Error(writer, err.Error(), http.StatusBadRequest)
		return
	}
	server.objects[metadata.Name] = content
	writeJSON(writer, http.StatusOK, server.objectResource(metadata.Name))
}

func newTestGCSCache(t *testing.T, cacheConfig config.GCSCacheConfig) (*GCSCache, *fakeGCSServer) {
	t.Helper()
	fakeServer := &fakeGCSServer{objects: map[string][]byte{}}
	httpServer := httptest.NewServer(fakeServer)
	t.Cleanup(httpServer.Close)
	t.Setenv("STORAGE_EMULATOR_HOST", httpServer.URL)

	cache, err := NewGCSCache(context.Background(), cacheConfig)
	require.NoError(t, err)
	return cache, fakeServer
}

func TestGCSCache_NewRequiresBucket(t *testing.T) {
	_, err := NewGCSCache(context.Background(), config.GCSCacheConfig{})
	assert.ErrorContains(t, err, "GCS bucket name is not set")
}

func TestGCSCache_BuildPath(t *testing.T) {
	testCases := []struct {
		name         string
		cacheConfig  config.GCSCacheConfig
		expectedPath string
	}{
		{
			name:         "shared cache without prefix",
			cacheConfig:  config.GCSCacheConfig{Bucket: testBucketName, SharedCache: true},
			expectedPath: "/path/key",
		},
		{
			name:         "shared cache with prefix",
			cacheConfig:  config.GCSCacheConfig{Bucket: testBucketName, Prefix: "/prefix/", SharedCache: true},
			expectedPath: "prefix/path/key",
		},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			cache, _ := newTestGCSCache(t, testCase.cacheConfig)
			assert.Equal(t, "gcs", cache.TypeName())
			assert.Equal(t, testCase.expectedPath, cache.buildPath("path", "key"))
		})
	}

	t.Run("workspace prefix", func(t *testing.T) {
		withoutPrefix, _ := newTestGCSCache(t, config.GCSCacheConfig{Bucket: testBucketName})
		assert.Equal(t, withoutPrefix.workspacePrefix+"/path/key", withoutPrefix.buildPath("path", "key"))

		withPrefix, _ := newTestGCSCache(t, config.GCSCacheConfig{Bucket: testBucketName, Prefix: "prefix"})
		assert.Equal(t, "prefix/"+withPrefix.workspacePrefix+"/path/key", withPrefix.buildPath("path", "key"))
	})
}

func TestGCSCache_SetGetSizeExistsDelete(t *testing.T) {
	ctx := context.Background()
	cache, _ := newTestGCSCache(t, config.GCSCacheConfig{Bucket: testBucketName, Prefix: "prefix", SharedCache: true})

	exists, err := cache.Exists(ctx, "path", "key")
	require.NoError(t, err)
	assert.False(t, exists)

	_, err = cache.Get(ctx, "path", "key")
	assert.Error(t, err)
	_, err = cache.Size(ctx, "path", "key")
	assert.Error(t, err)
	assert.Error(t, cache.Delete(ctx, "path", "key"))

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

func TestGCSCache_SetPropagatesReadError(t *testing.T) {
	cache, _ := newTestGCSCache(t, config.GCSCacheConfig{Bucket: testBucketName, SharedCache: true})
	err := cache.Set(context.Background(), "path", "key", &errorReader{})
	assert.ErrorContains(t, err, "failed to copy data to GCS")
}

func TestGCSCache_ListKeys(t *testing.T) {
	ctx := context.Background()
	cache, _ := newTestGCSCache(t, config.GCSCacheConfig{Bucket: testBucketName, Prefix: "prefix", SharedCache: true})

	for _, key := range []string{"day/a.parquet", "day/b.parquet", "day/c.txt"} {
		require.NoError(t, cache.Set(ctx, "traces", key, strings.NewReader(key)))
	}
	require.NoError(t, cache.Set(ctx, "other", "d.parquet", strings.NewReader("d")))

	testCases := []struct {
		name         string
		suffix       string
		expectedKeys []string
	}{
		{name: "no suffix", suffix: "", expectedKeys: []string{"day/a.parquet", "day/b.parquet", "day/c.txt"}},
		{name: "parquet suffix", suffix: ".parquet", expectedKeys: []string{"day/a.parquet", "day/b.parquet"}},
		{name: "no match", suffix: ".json", expectedKeys: nil},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			keys, err := cache.ListKeys(ctx, "traces", testCase.suffix)
			require.NoError(t, err)
			assert.Equal(t, testCase.expectedKeys, keys)
		})
	}
}

func TestGCSCache_StagedWrite(t *testing.T) {
	ctx := context.Background()

	t.Run("commit promotes staged object and removes staging", func(t *testing.T) {
		cache, fakeServer := newTestGCSCache(t, config.GCSCacheConfig{Bucket: testBucketName, SharedCache: true})
		stagedWriter, err := cache.BeginWrite(ctx)
		require.NoError(t, err)
		_, err = stagedWriter.Write([]byte("staged content"))
		require.NoError(t, err)
		require.NoError(t, stagedWriter.Commit(ctx, "cas", "digest"))

		reader, err := cache.Get(ctx, "cas", "digest")
		require.NoError(t, err)
		content, err := io.ReadAll(reader)
		require.NoError(t, err)
		require.NoError(t, reader.Close())
		assert.Equal(t, "staged content", string(content))
		assert.Len(t, fakeServer.objects, 1)

		assert.ErrorContains(t, stagedWriter.Commit(ctx, "cas", "digest"), "commit after commit/cancel")
		assert.NoError(t, stagedWriter.Cancel(ctx))
	})

	t.Run("cancel leaves no objects", func(t *testing.T) {
		cache, fakeServer := newTestGCSCache(t, config.GCSCacheConfig{Bucket: testBucketName, SharedCache: true})
		stagedWriter, err := cache.BeginWrite(ctx)
		require.NoError(t, err)
		_, err = stagedWriter.Write([]byte("discarded"))
		require.NoError(t, err)
		require.NoError(t, stagedWriter.Cancel(ctx))
		assert.NoError(t, stagedWriter.Cancel(ctx))
		assert.Empty(t, fakeServer.objects)
	})
}
