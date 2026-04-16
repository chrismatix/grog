package dockerproxy_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"grog/internal/caching"
	"grog/internal/caching/backends"
	"grog/internal/dockerproxy"
)

// newTestRegistry spins up a registry backed by a fresh on-disk filesystem CAS
// inside t.TempDir(). It returns the registry and the underlying CAS so tests
// can poke at storage directly.
func newTestRegistry(t *testing.T) (*dockerproxy.Registry, *caching.Cas) {
	t.Helper()
	ctx := context.Background()

	fs := backends.NewFileSystemCacheForTest(t.TempDir(), t.TempDir())
	cas := caching.NewCas(fs)

	reg, err := dockerproxy.New(ctx, cas)
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
func urlFor(reg *dockerproxy.Registry, path string) string {
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

	reg, err := dockerproxy.New(ctx, cas)
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
	for i := 0; i < sessions; i++ {
		payloads[i] = []byte(strings.Repeat(fmt.Sprintf("%c", 'a'+i), 4096))
		digests[i] = digestOf(payloads[i])
	}

	var wg sync.WaitGroup
	errs := make(chan error, sessions)

	for i := 0; i < sessions; i++ {
		i := i
		wg.Add(1)
		go func() {
			defer wg.Done()

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
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Errorf("session error: %v", err)
	}

	for i := 0; i < sessions; i++ {
		got, err := cas.LoadBytes(ctx, digests[i])
		require.NoError(t, err)
		assert.Equal(t, payloads[i], got, "session %d", i)
	}
}
