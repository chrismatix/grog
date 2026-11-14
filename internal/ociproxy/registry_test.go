package ociproxy_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/chrismatix/grog/internal/caching"
	"github.com/chrismatix/grog/internal/caching/backends"
	"github.com/chrismatix/grog/internal/ociproxy"
)

// newTestRegistry spins up a registry backed by a fresh on-disk filesystem CAS
// inside t.TempDir(). It returns the registry and the underlying CAS so tests
// can poke at storage directly.
func newTestRegistry(t *testing.T) (*ociproxy.Registry, *caching.Cas) {
	t.Helper()
	ctx := context.Background()

	fs := backends.NewFileSystemCacheForTest(t.TempDir(), t.TempDir())
	cas := caching.NewCas(fs)

	reg, err := ociproxy.New(ctx, cas)
	require.NoError(t, err)
	t.Cleanup(func() { _ = reg.Close() })

	return reg, cas
}

// digestOf returns the canonical sha256:<hex> digest of the given bytes.
func digestOf(b []byte) string {
	sum := sha256.Sum256(b)
	return "sha256:" + hex.EncodeToString(sum[:])
}

// urlFor builds a full URL against the registry under test.
func urlFor(reg *ociproxy.Registry, path string) string {
	return "http://" + reg.Addr() + path
}

func TestVersionProbe(t *testing.T) {
	reg, _ := newTestRegistry(t)

	for _, method := range []string{http.MethodGet, http.MethodHead} {
		t.Run(method, func(t *testing.T) {
			req, err := http.NewRequest(method, urlFor(reg, "/v2/"), nil)
			require.NoError(t, err)

			resp, err := http.DefaultClient.Do(req)
			require.NoError(t, err)
			defer resp.Body.Close()

			assert.Equal(t, http.StatusOK, resp.StatusCode)
			assert.Equal(t, "registry/2.0", resp.Header.Get("Docker-Distribution-API-Version"))
		})
	}
}

func TestHeadBlobMissingThenPresent(t *testing.T) {
	reg, cas := newTestRegistry(t)
	ctx := context.Background()

	payload := []byte("hello world")
	digest := digestOf(payload)

	// 404 before the blob exists.
	resp, err := http.Head(urlFor(reg, "/v2/foo/blobs/"+digest))
	require.NoError(t, err)
	resp.Body.Close()
	assert.Equal(t, http.StatusNotFound, resp.StatusCode)

	// Write directly into the CAS — simulates a prior cache hit.
	require.NoError(t, cas.WriteBytes(ctx, digest, payload))

	resp, err = http.Head(urlFor(reg, "/v2/foo/blobs/"+digest))
	require.NoError(t, err)
	resp.Body.Close()
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Equal(t, digest, resp.Header.Get("Docker-Content-Digest"))
	// Regression: the Docker daemon refuses HEAD responses without Content-Length
	// (it uses the value to size up the blob before deciding what to do next).
	assert.Equal(t, fmt.Sprintf("%d", len(payload)), resp.Header.Get("Content-Length"))
}

func TestGetBlobStreamsBody(t *testing.T) {
	reg, cas := newTestRegistry(t)
	ctx := context.Background()

	payload := bytes.Repeat([]byte("abcdef"), 4096) // ~24 KiB to make sure streaming works
	digest := digestOf(payload)
	require.NoError(t, cas.WriteBytes(ctx, digest, payload))

	resp, err := http.Get(urlFor(reg, "/v2/anything/blobs/"+digest))
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Equal(t, digest, resp.Header.Get("Docker-Content-Digest"))
	// Regression: the daemon expects an explicit Content-Length on blob GETs.
	// Without it Go falls back to chunked transfer encoding and the daemon
	// fails the pull with "missing content-length header for request".
	assert.Equal(t, fmt.Sprintf("%d", len(payload)), resp.Header.Get("Content-Length"))

	got, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	assert.Equal(t, payload, got)
}

func TestBlobBadDigestFormat(t *testing.T) {
	reg, _ := newTestRegistry(t)

	resp, err := http.Head(urlFor(reg, "/v2/foo/blobs/not-a-digest"))
	require.NoError(t, err)
	resp.Body.Close()
	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
}

func TestMonolithicBlobUpload(t *testing.T) {
	reg, cas := newTestRegistry(t)
	ctx := context.Background()

	payload := []byte("monolithic upload payload")
	digest := digestOf(payload)

	postURL := urlFor(reg, "/v2/some/repo/blobs/uploads/?digest="+digest)
	resp, err := http.Post(postURL, "application/octet-stream", bytes.NewReader(payload))
	require.NoError(t, err)
	resp.Body.Close()

	assert.Equal(t, http.StatusCreated, resp.StatusCode)
	assert.Equal(t, digest, resp.Header.Get("Docker-Content-Digest"))
	assert.Equal(t, "/v2/some/repo/blobs/"+digest, resp.Header.Get("Location"))

	exists, err := cas.Exists(ctx, digest)
	require.NoError(t, err)
	assert.True(t, exists, "blob should be in CAS after monolithic upload")

	got, err := cas.LoadBytes(ctx, digest)
	require.NoError(t, err)
	assert.Equal(t, payload, got)
}

func TestMonolithicBlobUploadInvalidDigest(t *testing.T) {
	reg, _ := newTestRegistry(t)

	resp, err := http.Post(
		urlFor(reg, "/v2/some/repo/blobs/uploads/?digest=garbage"),
		"application/octet-stream",
		strings.NewReader("body"),
	)
	require.NoError(t, err)
	resp.Body.Close()
	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
}

func TestChunkedBlobUpload(t *testing.T) {
	reg, cas := newTestRegistry(t)
	ctx := context.Background()

	payload := bytes.Repeat([]byte("0123456789"), 1500) // 15 KiB
	digest := digestOf(payload)

	// Step 1: POST start session
	startResp, err := http.Post(urlFor(reg, "/v2/some/repo/blobs/uploads/"), "application/octet-stream", nil)
	require.NoError(t, err)
	startResp.Body.Close()
	require.Equal(t, http.StatusAccepted, startResp.StatusCode)

	location := startResp.Header.Get("Location")
	require.NotEmpty(t, location)
	uploadUUID := startResp.Header.Get("Docker-Upload-UUID")
	require.NotEmpty(t, uploadUUID)

	// Step 2: PATCH a couple of chunks. The Location is a path, not a full URL.
	patchURL := urlFor(reg, location)
	chunks := [][]byte{
		payload[:5000],
		payload[5000:10000],
		payload[10000:],
	}
	for _, chunk := range chunks {
		req, err := http.NewRequest(http.MethodPatch, patchURL, bytes.NewReader(chunk))
		require.NoError(t, err)
		req.Header.Set("Content-Type", "application/octet-stream")
		resp, err := http.DefaultClient.Do(req)
		require.NoError(t, err)
		resp.Body.Close()
		assert.Equal(t, http.StatusAccepted, resp.StatusCode)
		assert.Equal(t, uploadUUID, resp.Header.Get("Docker-Upload-UUID"))
		assert.NotEmpty(t, resp.Header.Get("Range"))
	}

	// Step 3: PUT to finalise (no trailing body)
	putURL := patchURL + "?digest=" + digest
	req, err := http.NewRequest(http.MethodPut, putURL, nil)
	require.NoError(t, err)
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	resp.Body.Close()
	require.Equal(t, http.StatusCreated, resp.StatusCode)
	assert.Equal(t, digest, resp.Header.Get("Docker-Content-Digest"))

	// Verify CAS contents
	got, err := cas.LoadBytes(ctx, digest)
	require.NoError(t, err)
	assert.Equal(t, payload, got)
}

func TestChunkedBlobUploadDigestMismatch(t *testing.T) {
	reg, cas := newTestRegistry(t)
	ctx := context.Background()

	payload := []byte("abc")
	wrongDigest := digestOf([]byte("xyz"))

	startResp, err := http.Post(urlFor(reg, "/v2/some/repo/blobs/uploads/"), "application/octet-stream", nil)
	require.NoError(t, err)
	startResp.Body.Close()
	location := startResp.Header.Get("Location")

	patchReq, err := http.NewRequest(http.MethodPatch, urlFor(reg, location), bytes.NewReader(payload))
	require.NoError(t, err)
	patchResp, err := http.DefaultClient.Do(patchReq)
	require.NoError(t, err)
	patchResp.Body.Close()
	require.Equal(t, http.StatusAccepted, patchResp.StatusCode)

	putReq, err := http.NewRequest(http.MethodPut, urlFor(reg, location)+"?digest="+wrongDigest, nil)
	require.NoError(t, err)
	putResp, err := http.DefaultClient.Do(putReq)
	require.NoError(t, err)
	putResp.Body.Close()
	assert.Equal(t, http.StatusBadRequest, putResp.StatusCode)

	// CAS should NOT contain the wrong-digest blob
	exists, err := cas.Exists(ctx, wrongDigest)
	require.NoError(t, err)
	assert.False(t, exists)
}

func TestChunkedBlobUploadTrailingChunkInPut(t *testing.T) {
	reg, cas := newTestRegistry(t)
	ctx := context.Background()

	first := []byte("first half;")
	second := []byte("second half")
	full := append(append([]byte{}, first...), second...)
	digest := digestOf(full)

	// Start session
	startResp, err := http.Post(urlFor(reg, "/v2/some/repo/blobs/uploads/"), "application/octet-stream", nil)
	require.NoError(t, err)
	startResp.Body.Close()
	location := startResp.Header.Get("Location")

	// PATCH the first half
	patchReq, err := http.NewRequest(http.MethodPatch, urlFor(reg, location), bytes.NewReader(first))
	require.NoError(t, err)
	patchResp, err := http.DefaultClient.Do(patchReq)
	require.NoError(t, err)
	patchResp.Body.Close()

	// PUT with the second half as the body
	putReq, err := http.NewRequest(http.MethodPut, urlFor(reg, location)+"?digest="+digest, bytes.NewReader(second))
	require.NoError(t, err)
	putReq.ContentLength = int64(len(second))
	putResp, err := http.DefaultClient.Do(putReq)
	require.NoError(t, err)
	putResp.Body.Close()
	require.Equal(t, http.StatusCreated, putResp.StatusCode)

	got, err := cas.LoadBytes(ctx, digest)
	require.NoError(t, err)
	assert.Equal(t, full, got)
}

func TestPutManifestStoresByContentDigest(t *testing.T) {
	reg, cas := newTestRegistry(t)
	ctx := context.Background()

	manifest := []byte(`{"schemaVersion":2,"mediaType":"application/vnd.oci.image.manifest.v1+json","config":{"mediaType":"application/vnd.oci.image.config.v1+json","size":7023,"digest":"sha256:c1eaf0e7b8d4f9f7b5b3c1eaf0e7b8d4f9f7b5b3c1eaf0e7b8d4f9f7b5b3c1ea"},"layers":[]}`)
	expectedDigest := digestOf(manifest)

	req, err := http.NewRequest(http.MethodPut, urlFor(reg, "/v2/grog/myrepo/manifests/sometag"), bytes.NewReader(manifest))
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/vnd.oci.image.manifest.v1+json")
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	resp.Body.Close()

	assert.Equal(t, http.StatusCreated, resp.StatusCode)
	assert.Equal(t, expectedDigest, resp.Header.Get("Docker-Content-Digest"))
	assert.Equal(t, "/v2/grog/myrepo/manifests/"+expectedDigest, resp.Header.Get("Location"))

	got, err := cas.LoadBytes(ctx, expectedDigest)
	require.NoError(t, err)
	assert.Equal(t, manifest, got)

	// LastManifestDigest must reflect the digest just pushed.
	assert.Equal(t, expectedDigest, reg.LastManifestDigest("grog/myrepo"))
}

func TestGetManifestByDigest(t *testing.T) {
	reg, cas := newTestRegistry(t)
	ctx := context.Background()

	manifest := []byte(`{"schemaVersion":2,"mediaType":"application/vnd.docker.distribution.manifest.v2+json"}`)
	digest := digestOf(manifest)
	require.NoError(t, cas.WriteBytes(ctx, digest, manifest))

	resp, err := http.Get(urlFor(reg, "/v2/grog/myrepo/manifests/"+digest))
	require.NoError(t, err)
	defer resp.Body.Close()

	require.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Equal(t, digest, resp.Header.Get("Docker-Content-Digest"))
	assert.Equal(t, "application/vnd.docker.distribution.manifest.v2+json", resp.Header.Get("Content-Type"))
	assert.Equal(t, fmt.Sprintf("%d", len(manifest)), resp.Header.Get("Content-Length"))

	got, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	assert.Equal(t, manifest, got)
}

func TestGetManifestByTagFails(t *testing.T) {
	reg, _ := newTestRegistry(t)

	resp, err := http.Get(urlFor(reg, "/v2/grog/myrepo/manifests/latest"))
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusNotFound, resp.StatusCode)

	var body struct {
		Errors []struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"errors"`
	}
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&body))
	require.NotEmpty(t, body.Errors)
	assert.Equal(t, "MANIFEST_UNKNOWN", body.Errors[0].Code)
}

func TestNotFoundForUnknownPath(t *testing.T) {
	reg, _ := newTestRegistry(t)

	resp, err := http.Get(urlFor(reg, "/v2/foo/bar"))
	require.NoError(t, err)
	resp.Body.Close()
	assert.Equal(t, http.StatusNotFound, resp.StatusCode)
}

// ctxObservingBackend wraps a CacheBackend and hands every BeginWrite the
// caller's ctx via a StagedWriter that fails Write/Commit once that ctx is
// cancelled. It exists to reproduce the cloud-backend pattern (S3/GCS/Azure)
// where a background upload goroutine observes the BeginWrite ctx — the
// FileSystemCache used by newTestRegistry doesn't have this dependency, so
// the bug it guards against is invisible without a custom mock.
type ctxObservingBackend struct {
	backends.CacheBackend
}

func (b *ctxObservingBackend) BeginWrite(ctx context.Context) (backends.StagedWriter, error) {
	inner, err := b.CacheBackend.BeginWrite(ctx)
	if err != nil {
		return nil, err
	}
	return &ctxObservingStagedWriter{ctx: ctx, inner: inner}, nil
}

type ctxObservingStagedWriter struct {
	ctx   context.Context
	inner backends.StagedWriter
}

func (w *ctxObservingStagedWriter) Write(p []byte) (int, error) {
	if err := w.ctx.Err(); err != nil {
		return 0, fmt.Errorf("ctx-aware staged writer write: %w", err)
	}
	return w.inner.Write(p)
}

func (w *ctxObservingStagedWriter) Commit(ctx context.Context, path, key string) error {
	if err := w.ctx.Err(); err != nil {
		return fmt.Errorf("ctx-aware staged writer commit: %w", err)
	}
	return w.inner.Commit(ctx, path, key)
}

func (w *ctxObservingStagedWriter) Cancel(ctx context.Context) error {
	return w.inner.Cancel(ctx)
}

// TestRegistry_ChunkedUploadSurvivesPostContextCancellation is a regression
// test for the bug where openUpload was passing the POST request's context
// to cas.BeginWrite. net/http cancels the request's ctx as soon as the
// handler returns, so any backend that captured the ctx (S3, GCS, Azure)
// would tear its upload goroutine down before the daemon's first PATCH
// arrived.
//
// This test uses a backend whose StagedWriter checks ctx.Err() on every
// Write to surface the bug — without the fix, the test fails on PATCH.
func TestRegistry_ChunkedUploadSurvivesPostContextCancellation(t *testing.T) {
	ctx := context.Background()
	fs := backends.NewFileSystemCacheForTest(t.TempDir(), t.TempDir())
	cas := caching.NewCas(&ctxObservingBackend{CacheBackend: fs})

	reg, err := ociproxy.New(ctx, cas)
	require.NoError(t, err)
	t.Cleanup(func() { _ = reg.Close() })

	// POST: open chunked session
	startResp, err := http.Post(urlFor(reg, "/v2/some/repo/blobs/uploads/"), "application/octet-stream", nil)
	require.NoError(t, err)
	startResp.Body.Close()
	require.Equal(t, http.StatusAccepted, startResp.StatusCode)
	location := startResp.Header.Get("Location")

	// PATCH a chunk in a *separate* HTTP request — at this point the POST
	// request's context is already cancelled by net/http. If openUpload
	// captured req.Context() into the staged writer, this Write would fail
	// with "context canceled" and the daemon would never reach a successful
	// PUT.
	payload := []byte("post-context-cancellation payload")
	digest := digestOf(payload)

	patchReq, err := http.NewRequest(http.MethodPatch, urlFor(reg, location), bytes.NewReader(payload))
	require.NoError(t, err)
	patchResp, err := http.DefaultClient.Do(patchReq)
	require.NoError(t, err)
	patchBody, _ := io.ReadAll(patchResp.Body)
	patchResp.Body.Close()
	require.Equal(t, http.StatusAccepted, patchResp.StatusCode, "PATCH body: %s", patchBody)

	// PUT to finalize
	putReq, err := http.NewRequest(http.MethodPut, urlFor(reg, location)+"?digest="+digest, nil)
	require.NoError(t, err)
	putResp, err := http.DefaultClient.Do(putReq)
	require.NoError(t, err)
	putBody, _ := io.ReadAll(putResp.Body)
	putResp.Body.Close()
	require.Equal(t, http.StatusCreated, putResp.StatusCode, "PUT body: %s", putBody)
}

func TestConcurrentChunkedUploadsAreIsolated(t *testing.T) {
	reg, cas := newTestRegistry(t)
	ctx := context.Background()

	const sessions = 8
	payloads := make([][]byte, sessions)
	digests := make([]string, sessions)
	for i := range sessions {
		payloads[i] = []byte(strings.Repeat(fmt.Sprintf("%c", 'a'+i), 4096))
		digests[i] = digestOf(payloads[i])
	}

	var wg sync.WaitGroup
	errs := make(chan error, sessions)

	for i := range sessions {
		wg.Go(func() {
			startResp, err := http.Post(urlFor(reg, "/v2/repo/blobs/uploads/"), "application/octet-stream", nil)
			if err != nil {
				errs <- err
				return
			}
			startResp.Body.Close()
			location := startResp.Header.Get("Location")

			patchReq, err := http.NewRequest(http.MethodPatch, urlFor(reg, location), bytes.NewReader(payloads[i]))
			if err != nil {
				errs <- err
				return
			}
			patchResp, err := http.DefaultClient.Do(patchReq)
			if err != nil {
				errs <- err
				return
			}
			patchResp.Body.Close()

			putReq, err := http.NewRequest(http.MethodPut, urlFor(reg, location)+"?digest="+digests[i], nil)
			if err != nil {
				errs <- err
				return
			}
			putResp, err := http.DefaultClient.Do(putReq)
			if err != nil {
				errs <- err
				return
			}
			putResp.Body.Close()
			if putResp.StatusCode != http.StatusCreated {
				errs <- fmt.Errorf("session %d: unexpected status %d", i, putResp.StatusCode)
				return
			}
		})
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Errorf("session error: %v", err)
	}

	for i := range sessions {
		got, err := cas.LoadBytes(ctx, digests[i])
		require.NoError(t, err)
		assert.Equal(t, payloads[i], got, "session %d", i)
	}
}

func sendRequest(t *testing.T, method, url string, body []byte) *http.Response {
	t.Helper()
	request, err := http.NewRequest(method, url, bytes.NewReader(body))
	require.NoError(t, err)
	response, err := http.DefaultClient.Do(request)
	require.NoError(t, err)
	response.Body.Close()
	return response
}

func startUploadSession(t *testing.T, registry *ociproxy.Registry) string {
	t.Helper()
	response := sendRequest(t, http.MethodPost, urlFor(registry, "/v2/repo/blobs/uploads/"), nil)
	require.Equal(t, http.StatusAccepted, response.StatusCode)
	return response.Header.Get("Location")
}

func TestRequestErrors(t *testing.T) {
	registry, _ := newTestRegistry(t)
	payload := []byte("payload")
	digest := digestOf(payload)

	testCases := []struct {
		name           string
		method         string
		path           string
		body           []byte
		expectedStatus int
	}{
		{"get missing blob", http.MethodGet, "/v2/repo/blobs/" + digest, nil, http.StatusNotFound},
		{"monolithic digest mismatch", http.MethodPost, "/v2/repo/blobs/uploads/?digest=" + digestOf([]byte("other")), payload, http.StatusInternalServerError},
		{"patch without upload id", http.MethodPatch, "/v2/repo/blobs/uploads/", payload, http.StatusBadRequest},
		{"patch unknown upload id", http.MethodPatch, "/v2/repo/blobs/uploads/unknown", payload, http.StatusNotFound},
		{"put without upload id", http.MethodPut, "/v2/repo/blobs/uploads/?digest=" + digest, nil, http.StatusBadRequest},
		{"put invalid digest", http.MethodPut, "/v2/repo/blobs/uploads/unknown?digest=garbage", nil, http.StatusBadRequest},
		{"put unknown upload id", http.MethodPut, "/v2/repo/blobs/uploads/unknown?digest=" + digest, nil, http.StatusNotFound},
		{"delete blob not allowed", http.MethodDelete, "/v2/repo/blobs/" + digest, nil, http.StatusMethodNotAllowed},
		{"delete upload not allowed", http.MethodDelete, "/v2/repo/blobs/uploads/unknown", nil, http.StatusMethodNotAllowed},
		{"delete manifest not allowed", http.MethodDelete, "/v2/repo/manifests/" + digest, nil, http.StatusMethodNotAllowed},
		{"get missing manifest", http.MethodGet, "/v2/repo/manifests/" + digest, nil, http.StatusNotFound},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			response := sendRequest(t, testCase.method, urlFor(registry, testCase.path), testCase.body)
			assert.Equal(t, testCase.expectedStatus, response.StatusCode)
		})
	}
}

var errInjected = errors.New("injected failure")

// faultyBackend fails the configured operations and records staged-writer cancellations.
type faultyBackend struct {
	backends.CacheBackend

	existsError     error
	sizeError       error
	getError        error
	beginWriteError error
	writeError      error
	commitError     error
	cancelCount     atomic.Int32
}

func (backend *faultyBackend) Exists(ctx context.Context, path, key string) (bool, error) {
	if backend.existsError != nil {
		return false, backend.existsError
	}
	return backend.CacheBackend.Exists(ctx, path, key)
}

func (backend *faultyBackend) Size(ctx context.Context, path, key string) (int64, error) {
	if backend.sizeError != nil {
		return 0, backend.sizeError
	}
	return backend.CacheBackend.Size(ctx, path, key)
}

func (backend *faultyBackend) Get(ctx context.Context, path, key string) (io.ReadCloser, error) {
	if backend.getError != nil {
		return nil, backend.getError
	}
	return backend.CacheBackend.Get(ctx, path, key)
}

func (backend *faultyBackend) BeginWrite(ctx context.Context) (backends.StagedWriter, error) {
	if backend.beginWriteError != nil {
		return nil, backend.beginWriteError
	}
	inner, err := backend.CacheBackend.BeginWrite(ctx)
	if err != nil {
		return nil, err
	}
	return &faultyStagedWriter{backend: backend, inner: inner}, nil
}

type faultyStagedWriter struct {
	backend *faultyBackend
	inner   backends.StagedWriter
}

func (writer *faultyStagedWriter) Write(content []byte) (int, error) {
	if writer.backend.writeError != nil {
		return 0, writer.backend.writeError
	}
	return writer.inner.Write(content)
}

func (writer *faultyStagedWriter) Commit(ctx context.Context, path, key string) error {
	if writer.backend.commitError != nil {
		return errors.Join(writer.backend.commitError, writer.inner.Cancel(ctx))
	}
	return writer.inner.Commit(ctx, path, key)
}

func (writer *faultyStagedWriter) Cancel(ctx context.Context) error {
	writer.backend.cancelCount.Add(1)
	return writer.inner.Cancel(ctx)
}

func newFaultyRegistry(t *testing.T, backend *faultyBackend) *ociproxy.Registry {
	t.Helper()
	backend.CacheBackend = backends.NewFileSystemCacheForTest(t.TempDir(), t.TempDir())
	registry, err := ociproxy.New(context.Background(), caching.NewCas(backend))
	require.NoError(t, err)
	t.Cleanup(func() { _ = registry.Close() })
	return registry
}

func TestBackendFailures(t *testing.T) {
	payload := []byte("payload")
	digest := digestOf(payload)

	testCases := []struct {
		name           string
		backend        *faultyBackend
		storeBlob      bool
		startSession   bool
		method         string
		path           string
		expectedStatus int
	}{
		{name: "head exists error", backend: &faultyBackend{existsError: errInjected}, method: http.MethodHead, path: "/v2/repo/blobs/" + digest, expectedStatus: http.StatusInternalServerError},
		{name: "head size error", backend: &faultyBackend{sizeError: errInjected}, storeBlob: true, method: http.MethodHead, path: "/v2/repo/blobs/" + digest, expectedStatus: http.StatusInternalServerError},
		{name: "get load error", backend: &faultyBackend{getError: errInjected}, storeBlob: true, method: http.MethodGet, path: "/v2/repo/blobs/" + digest, expectedStatus: http.StatusNotFound},
		{name: "manifest exists error", backend: &faultyBackend{existsError: errInjected}, method: http.MethodGet, path: "/v2/repo/manifests/" + digest, expectedStatus: http.StatusInternalServerError},
		{name: "manifest load error", backend: &faultyBackend{getError: errInjected}, storeBlob: true, method: http.MethodGet, path: "/v2/repo/manifests/" + digest, expectedStatus: http.StatusInternalServerError},
		{name: "monolithic begin write error", backend: &faultyBackend{beginWriteError: errInjected}, method: http.MethodPost, path: "/v2/repo/blobs/uploads/?digest=" + digest, expectedStatus: http.StatusInternalServerError},
		{name: "monolithic write error", backend: &faultyBackend{writeError: errInjected}, method: http.MethodPost, path: "/v2/repo/blobs/uploads/?digest=" + digest, expectedStatus: http.StatusInternalServerError},
		{name: "monolithic commit error", backend: &faultyBackend{commitError: errInjected}, method: http.MethodPost, path: "/v2/repo/blobs/uploads/?digest=" + digest, expectedStatus: http.StatusInternalServerError},
		{name: "chunked begin write error", backend: &faultyBackend{beginWriteError: errInjected}, method: http.MethodPost, path: "/v2/repo/blobs/uploads/", expectedStatus: http.StatusInternalServerError},
		{name: "chunked patch write error", backend: &faultyBackend{writeError: errInjected}, startSession: true, method: http.MethodPatch, expectedStatus: http.StatusInternalServerError},
		{name: "chunked trailing write error", backend: &faultyBackend{writeError: errInjected}, startSession: true, method: http.MethodPut, path: "?digest=" + digest, expectedStatus: http.StatusInternalServerError},
		{name: "chunked commit error", backend: &faultyBackend{commitError: errInjected}, startSession: true, method: http.MethodPut, path: "?digest=" + digest, expectedStatus: http.StatusInternalServerError},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			registry := newFaultyRegistry(t, testCase.backend)
			if testCase.storeBlob {
				require.NoError(t, testCase.backend.Set(context.Background(), "cas", digest, bytes.NewReader(payload)))
			}
			path := testCase.path
			if testCase.startSession {
				path = startUploadSession(t, registry) + path
			}
			response := sendRequest(t, testCase.method, urlFor(registry, path), payload)
			assert.Equal(t, testCase.expectedStatus, response.StatusCode)
		})
	}
}

func TestFailedPatchCancelsSession(t *testing.T) {
	backend := &faultyBackend{writeError: errInjected}
	registry := newFaultyRegistry(t, backend)
	location := startUploadSession(t, registry)

	response := sendRequest(t, http.MethodPatch, urlFor(registry, location), []byte("chunk"))
	assert.Equal(t, http.StatusInternalServerError, response.StatusCode)
	assert.Equal(t, int32(1), backend.cancelCount.Load())

	response = sendRequest(t, http.MethodPatch, urlFor(registry, location), []byte("chunk"))
	assert.Equal(t, http.StatusNotFound, response.StatusCode)
}

func TestCloseCancelsPendingUploads(t *testing.T) {
	backend := &faultyBackend{}
	registry := newFaultyRegistry(t, backend)
	startUploadSession(t, registry)
	startUploadSession(t, registry)

	require.NoError(t, registry.Close())
	assert.Equal(t, int32(2), backend.cancelCount.Load())
	require.NoError(t, registry.Close())
	assert.Equal(t, int32(2), backend.cancelCount.Load())
}
