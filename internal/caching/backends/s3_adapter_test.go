package backends

import (
	"context"
	"encoding/xml"
	"io"
	"net/http"
	"net/http/httptest"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/chrismatix/grog/internal/config"
)

// fakeS3Server serves the path-style subset of the S3 REST API that AWSS3Adapter uses.
type fakeS3Server struct {
	mutex   sync.Mutex
	objects map[string][]byte
}

type listBucketResult struct {
	XMLName  xml.Name `xml:"ListBucketResult"`
	Contents []struct {
		Key  string `xml:"Key"`
		Size int    `xml:"Size"`
	} `xml:"Contents"`
}

func (server *fakeS3Server) ServeHTTP(writer http.ResponseWriter, request *http.Request) {
	server.mutex.Lock()
	defer server.mutex.Unlock()

	bucketAndKey := strings.SplitN(strings.TrimPrefix(request.URL.Path, "/"), "/", 2)
	if len(bucketAndKey) == 1 || bucketAndKey[1] == "" {
		prefix := request.URL.Query().Get("prefix")
		var result listBucketResult
		var keys []string
		for key := range server.objects {
			if strings.HasPrefix(key, prefix) {
				keys = append(keys, key)
			}
		}
		sort.Strings(keys)
		for _, key := range keys {
			result.Contents = append(result.Contents, struct {
				Key  string `xml:"Key"`
				Size int    `xml:"Size"`
			}{Key: key, Size: len(server.objects[key])})
		}
		writer.Header().Set("Content-Type", "application/xml")
		_ = xml.NewEncoder(writer).Encode(result)
		return
	}
	key := bucketAndKey[1]

	switch request.Method {
	case http.MethodPut:
		if copySource := request.Header.Get("X-Amz-Copy-Source"); copySource != "" {
			sourceKey := strings.SplitN(strings.TrimPrefix(copySource, "/"), "/", 2)[1]
			server.objects[key] = append([]byte(nil), server.objects[sourceKey]...)
			writer.Header().Set("Content-Type", "application/xml")
			_, _ = io.WriteString(writer, `<CopyObjectResult><ETag>"etag"</ETag></CopyObjectResult>`)
			return
		}
		content, err := io.ReadAll(request.Body)
		if err != nil {
			http.Error(writer, err.Error(), http.StatusBadRequest)
			return
		}
		server.objects[key] = content
		writer.Header().Set("ETag", `"etag"`)
	case http.MethodDelete:
		delete(server.objects, key)
		writer.WriteHeader(http.StatusNoContent)
	case http.MethodHead, http.MethodGet:
		content, found := server.objects[key]
		if !found {
			writer.Header().Set("Content-Type", "application/xml")
			writer.WriteHeader(http.StatusNotFound)
			if request.Method == http.MethodGet {
				_, _ = io.WriteString(writer, `<Error><Code>NoSuchKey</Code><Message>missing</Message></Error>`)
			}
			return
		}
		writer.Header().Set("Content-Length", strconv.Itoa(len(content)))
		if request.Method == http.MethodGet {
			_, _ = writer.Write(content)
		}
	}
}

func newTestS3Cache(t *testing.T) (*S3Cache, *fakeS3Server) {
	t.Helper()
	fakeServer := &fakeS3Server{objects: map[string][]byte{}}
	httpServer := httptest.NewServer(fakeServer)
	t.Cleanup(httpServer.Close)

	client := s3.New(s3.Options{
		Region:       "us-east-1",
		BaseEndpoint: aws.String(httpServer.URL),
		UsePathStyle: true,
		Credentials: aws.CredentialsProviderFunc(func(context.Context) (aws.Credentials, error) {
			return aws.Credentials{AccessKeyID: "access", SecretAccessKey: "secret"}, nil
		}),
	})
	cache, err := NewS3CacheWithClient(context.Background(), config.S3CacheConfig{
		Bucket:      "test-bucket",
		Prefix:      "prefix",
		SharedCache: true,
	}, NewAWSS3Adapter(client))
	require.NoError(t, err)
	return cache, fakeServer
}

func TestNewS3Cache(t *testing.T) {
	t.Setenv("AWS_REGION", "us-east-1")
	t.Setenv("AWS_CONFIG_FILE", t.TempDir()+"/missing")
	t.Setenv("AWS_SHARED_CREDENTIALS_FILE", t.TempDir()+"/missing")

	_, err := NewS3Cache(context.Background(), config.S3CacheConfig{})
	assert.ErrorContains(t, err, "s3 bucket name is not set")

	cache, err := NewS3Cache(context.Background(), config.S3CacheConfig{Bucket: "test-bucket"})
	require.NoError(t, err)
	assert.IsType(t, &AWSS3Adapter{}, cache.client)
}

func TestAWSS3Adapter_RoundTrip(t *testing.T) {
	ctx := context.Background()
	cache, _ := newTestS3Cache(t)

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

func TestAWSS3Adapter_StagedWriteAndListKeys(t *testing.T) {
	ctx := context.Background()
	cache, fakeServer := newTestS3Cache(t)

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

func TestS3Cache_ListKeysWithoutAdapter(t *testing.T) {
	cache, err := NewS3CacheWithClient(context.Background(), config.S3CacheConfig{Bucket: "test-bucket"}, newMockS3Client())
	require.NoError(t, err)
	keys, err := cache.ListKeys(context.Background(), "traces", "")
	assert.NoError(t, err)
	assert.Nil(t, keys)
}
