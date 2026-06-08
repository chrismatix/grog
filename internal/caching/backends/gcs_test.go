package backends

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"sync"
	"testing"

	"cloud.google.com/go/storage"
	"google.golang.org/api/iterator"

	"grog/internal/config"
)

func TestGCSCache_TypeName(t *testing.T) {
	c := &GCSCache{}
	if c.TypeName() != "gcs" {
		t.Fatalf("TypeName = %q want gcs", c.TypeName())
	}
}

func TestGCSCache_BuildPath(t *testing.T) {
	tests := []struct {
		name            string
		prefix          string
		workspacePrefix string
		path            string
		key             string
		want            string
	}{
		{"both", "p", "w", "path", "key", "p/w/path/key"},
		{"prefix only", "p", "", "path", "key", "p/path/key"},
		{"workspace only", "", "w", "path", "key", "w/path/key"},
		{"trims slashes", "p", "w", "/path/", "/key/", "p/w/path/key"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			c := &GCSCache{prefix: tc.prefix, workspacePrefix: tc.workspacePrefix}
			got := c.buildPath(tc.path, tc.key)
			if got != tc.want {
				t.Fatalf("buildPath = %q want %q", got, tc.want)
			}
		})
	}
}

func TestGCSCache_FullPrefix(t *testing.T) {
	cases := []struct {
		prefix, workspace, want string
	}{
		{"", "w", "w"},
		{"p", "", "p"},
		{"p", "w", "p/w"},
		{"", "", ""},
	}
	for _, c := range cases {
		gcs := &GCSCache{prefix: c.prefix, workspacePrefix: c.workspace}
		if got := gcs.fullPrefix(); got != c.want {
			t.Fatalf("fullPrefix prefix=%q ws=%q got %q want %q", c.prefix, c.workspace, got, c.want)
		}
	}
}

func TestNewGCSCache_EmptyBucket(t *testing.T) {
	if _, err := NewGCSCache(context.Background(), config.GCSCacheConfig{}); err == nil {
		t.Fatal("expected error when bucket is empty")
	}
}

// mockGCSClient is an in-memory GCSClient used by GCSCache unit tests.
type mockGCSClient struct {
	mu          sync.Mutex
	objects     map[string][]byte
	failCopy    bool
	failDelete  bool
	failAttrsAs error
	failList    error
}

func newMockGCSClient() *mockGCSClient {
	return &mockGCSClient{objects: map[string][]byte{}}
}

func (m *mockGCSClient) NewReader(_ context.Context, _, object string) (io.ReadCloser, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	data, ok := m.objects[object]
	if !ok {
		return nil, storage.ErrObjectNotExist
	}
	return io.NopCloser(bytes.NewReader(data)), nil
}

func (m *mockGCSClient) NewWriter(_ context.Context, _, object string) io.WriteCloser {
	return &mockGCSWriter{m: m, object: object}
}

func (m *mockGCSClient) Delete(_ context.Context, _, object string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.failDelete {
		return errors.New("delete failed")
	}
	if _, ok := m.objects[object]; !ok {
		return storage.ErrObjectNotExist
	}
	delete(m.objects, object)
	return nil
}

func (m *mockGCSClient) Attrs(_ context.Context, _, object string) (int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.failAttrsAs != nil {
		return 0, m.failAttrsAs
	}
	data, ok := m.objects[object]
	if !ok {
		return 0, storage.ErrObjectNotExist
	}
	return int64(len(data)), nil
}

func (m *mockGCSClient) Copy(_ context.Context, _, srcObject, dstObject string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.failCopy {
		return errors.New("copy failed")
	}
	data, ok := m.objects[srcObject]
	if !ok {
		return storage.ErrObjectNotExist
	}
	dup := make([]byte, len(data))
	copy(dup, data)
	m.objects[dstObject] = dup
	return nil
}

func (m *mockGCSClient) List(_ context.Context, _, prefix string) GCSObjectIterator {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.failList != nil {
		return &errIterator{err: m.failList}
	}
	var names []string
	for name := range m.objects {
		if strings.HasPrefix(name, prefix) {
			names = append(names, name)
		}
	}
	return &sliceIterator{names: names}
}

type mockGCSWriter struct {
	m      *mockGCSClient
	object string
	buf    bytes.Buffer
	closed bool
}

func (w *mockGCSWriter) Write(p []byte) (int, error) {
	if w.closed {
		return 0, errors.New("write after close")
	}
	return w.buf.Write(p)
}

func (w *mockGCSWriter) Close() error {
	if w.closed {
		return nil
	}
	w.closed = true
	w.m.mu.Lock()
	defer w.m.mu.Unlock()
	w.m.objects[w.object] = w.buf.Bytes()
	return nil
}

type sliceIterator struct {
	names []string
	idx   int
}

func (s *sliceIterator) NextName() (string, error) {
	if s.idx >= len(s.names) {
		return "", iterator.Done
	}
	n := s.names[s.idx]
	s.idx++
	return n, nil
}

type errIterator struct{ err error }

func (e *errIterator) NextName() (string, error) { return "", e.err }

func newGCSCacheForTest(t *testing.T, mock GCSClient, prefix string) *GCSCache {
	t.Helper()
	prev := config.Global
	config.Global = config.WorkspaceConfig{WorkspaceRoot: "/tmp/test-ws"}
	t.Cleanup(func() { config.Global = prev })
	cache, err := NewGCSCacheWithClient(context.Background(), config.GCSCacheConfig{
		Bucket:      "buck",
		Prefix:      prefix,
		SharedCache: true,
	}, mock)
	if err != nil {
		t.Fatalf("NewGCSCacheWithClient: %v", err)
	}
	return cache
}

func TestGCSCache_SetGet(t *testing.T) {
	mock := newMockGCSClient()
	cache := newGCSCacheForTest(t, mock, "p")
	ctx := context.Background()

	if err := cache.Set(ctx, "path", "key", strings.NewReader("hello")); err != nil {
		t.Fatalf("Set: %v", err)
	}
	if string(mock.objects["p/path/key"]) != "hello" {
		t.Fatalf("not stored: %v", mock.objects)
	}

	r, err := cache.Get(ctx, "path", "key")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	defer r.Close()
	body, _ := io.ReadAll(r)
	if string(body) != "hello" {
		t.Fatalf("got %q", body)
	}
}

func TestGCSCache_GetMissing(t *testing.T) {
	cache := newGCSCacheForTest(t, newMockGCSClient(), "p")
	if _, err := cache.Get(context.Background(), "path", "missing"); err == nil {
		t.Fatal("expected error")
	}
}

func TestGCSCache_Delete(t *testing.T) {
	mock := newMockGCSClient()
	mock.objects["p/path/key"] = []byte("data")
	cache := newGCSCacheForTest(t, mock, "p")

	if err := cache.Delete(context.Background(), "path", "key"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, ok := mock.objects["p/path/key"]; ok {
		t.Fatal("not deleted")
	}
	if err := cache.Delete(context.Background(), "path", "missing"); err == nil {
		t.Fatal("expected err for missing")
	}
}

func TestGCSCache_Size(t *testing.T) {
	mock := newMockGCSClient()
	mock.objects["p/path/key"] = []byte("hello!")
	cache := newGCSCacheForTest(t, mock, "p")

	size, err := cache.Size(context.Background(), "path", "key")
	if err != nil {
		t.Fatalf("Size: %v", err)
	}
	if size != 6 {
		t.Fatalf("size %d", size)
	}
	if _, err := cache.Size(context.Background(), "path", "missing"); err == nil {
		t.Fatal("expected err")
	}
}

func TestGCSCache_Exists(t *testing.T) {
	mock := newMockGCSClient()
	mock.objects["p/path/key"] = []byte("data")
	cache := newGCSCacheForTest(t, mock, "p")
	ctx := context.Background()

	if exists, err := cache.Exists(ctx, "path", "key"); err != nil || !exists {
		t.Fatalf("Exists: %v %v", exists, err)
	}
	if exists, err := cache.Exists(ctx, "path", "missing"); err != nil || exists {
		t.Fatalf("Exists missing: %v %v", exists, err)
	}

	mock.failAttrsAs = errors.New("boom")
	if _, err := cache.Exists(ctx, "path", "key"); err == nil {
		t.Fatal("expected propagated err")
	}
}

func TestGCSCache_ListKeys(t *testing.T) {
	mock := newMockGCSClient()
	mock.objects["p/path/a.parquet"] = []byte("1")
	mock.objects["p/path/b.parquet"] = []byte("2")
	mock.objects["p/path/c.txt"] = []byte("3")
	mock.objects["p/other/d.parquet"] = []byte("4")
	cache := newGCSCacheForTest(t, mock, "p")
	ctx := context.Background()

	keys, err := cache.ListKeys(ctx, "path", "")
	if err != nil {
		t.Fatalf("ListKeys: %v", err)
	}
	if len(keys) != 3 {
		t.Fatalf("got %v", keys)
	}

	parquet, err := cache.ListKeys(ctx, "path", ".parquet")
	if err != nil {
		t.Fatalf("ListKeys suffix: %v", err)
	}
	if len(parquet) != 2 {
		t.Fatalf("got %v", parquet)
	}

	mock.failList = errors.New("list failed")
	if _, err := cache.ListKeys(ctx, "path", ""); err == nil {
		t.Fatal("expected propagated err")
	}
}

func TestGCSCache_BeginWriteCommit(t *testing.T) {
	mock := newMockGCSClient()
	cache := newGCSCacheForTest(t, mock, "p")
	ctx := context.Background()

	sw, err := cache.BeginWrite(ctx)
	if err != nil {
		t.Fatalf("BeginWrite: %v", err)
	}
	if _, err := sw.Write([]byte("payload")); err != nil {
		t.Fatalf("Write: %v", err)
	}
	if err := sw.Commit(ctx, "path", "final"); err != nil {
		t.Fatalf("Commit: %v", err)
	}
	if string(mock.objects["p/path/final"]) != "payload" {
		t.Fatalf("final not stored: %v", mock.objects)
	}
	for k := range mock.objects {
		if strings.Contains(k, gcsStagingPath) {
			t.Fatalf("staging not cleaned: %s", k)
		}
	}

	if err := sw.Commit(ctx, "path", "other"); err == nil {
		t.Fatal("double commit should fail")
	}
}

func TestGCSCache_BeginWriteCommit_CopyFails(t *testing.T) {
	mock := newMockGCSClient()
	mock.failCopy = true
	cache := newGCSCacheForTest(t, mock, "p")
	ctx := context.Background()

	sw, err := cache.BeginWrite(ctx)
	if err != nil {
		t.Fatalf("BeginWrite: %v", err)
	}
	if _, err := sw.Write([]byte("data")); err != nil {
		t.Fatal(err)
	}
	if err := sw.Commit(ctx, "path", "final"); err == nil {
		t.Fatal("expected copy err")
	}
}

func TestGCSCache_BeginWriteCancel(t *testing.T) {
	mock := newMockGCSClient()
	cache := newGCSCacheForTest(t, mock, "p")
	ctx := context.Background()

	sw, err := cache.BeginWrite(ctx)
	if err != nil {
		t.Fatalf("BeginWrite: %v", err)
	}
	_, _ = sw.Write([]byte("partial"))
	if err := sw.Cancel(ctx); err != nil {
		t.Fatalf("Cancel: %v", err)
	}
	// Cancel must be idempotent.
	if err := sw.Cancel(ctx); err != nil {
		t.Fatalf("second Cancel: %v", err)
	}
}

func TestGCSCache_SharedCache(t *testing.T) {
	prev := config.Global
	config.Global = config.WorkspaceConfig{WorkspaceRoot: "/tmp/abc"}
	t.Cleanup(func() { config.Global = prev })

	mock := newMockGCSClient()
	shared, err := NewGCSCacheWithClient(context.Background(), config.GCSCacheConfig{
		Bucket: "b", Prefix: "p", SharedCache: true,
	}, mock)
	if err != nil {
		t.Fatal(err)
	}
	if shared.workspacePrefix != "" {
		t.Fatal("shared should drop workspace prefix")
	}

	isolated, err := NewGCSCacheWithClient(context.Background(), config.GCSCacheConfig{
		Bucket: "b", Prefix: "p", SharedCache: false,
	}, mock)
	if err != nil {
		t.Fatal(err)
	}
	if isolated.workspacePrefix == "" {
		t.Fatal("non-shared should derive workspace prefix")
	}
}

func TestNewGCSCacheWithClient_EmptyBucket(t *testing.T) {
	if _, err := NewGCSCacheWithClient(context.Background(), config.GCSCacheConfig{}, newMockGCSClient()); err == nil {
		t.Fatal("expected err")
	}
}
