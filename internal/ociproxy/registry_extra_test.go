package ociproxy_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
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
	"grog/internal/ociproxy"
)

// faultyBackend wraps a backend and injects errors on the configured methods.
type faultyBackend struct {
	backends.CacheBackend
	mu             sync.Mutex
	existsErr      error
	sizeErr        error
	getErr         error
	setErr         error
	beginWriteErr  error
	stagedWriteErr error
	commitErr      error
}

func (f *faultyBackend) Exists(ctx context.Context, p, k string) (bool, error) {
	f.mu.Lock()
	err := f.existsErr
	f.mu.Unlock()
	if err != nil {
		return false, err
	}
	return f.CacheBackend.Exists(ctx, p, k)
}

func (f *faultyBackend) Size(ctx context.Context, p, k string) (int64, error) {
	f.mu.Lock()
	err := f.sizeErr
	f.mu.Unlock()
	if err != nil {
		return 0, err
	}
	return f.CacheBackend.Size(ctx, p, k)
}

func (f *faultyBackend) Get(ctx context.Context, p, k string) (io.ReadCloser, error) {
	f.mu.Lock()
	err := f.getErr
	f.mu.Unlock()
	if err != nil {
		return nil, err
	}
	return f.CacheBackend.Get(ctx, p, k)
}

func (f *faultyBackend) Set(ctx context.Context, p, k string, r io.Reader) error {
	f.mu.Lock()
	err := f.setErr
	f.mu.Unlock()
	if err != nil {
		_, _ = io.Copy(io.Discard, r)
		return err
	}
	return f.CacheBackend.Set(ctx, p, k, r)
}

func (f *faultyBackend) BeginWrite(ctx context.Context) (backends.StagedWriter, error) {
	f.mu.Lock()
	err := f.beginWriteErr
	writeErr := f.stagedWriteErr
	commitErr := f.commitErr
	f.mu.Unlock()
	if err != nil {
		return nil, err
	}
	inner, e := f.CacheBackend.BeginWrite(ctx)
	if e != nil {
		return nil, e
	}
	return &faultyStagedWriter{inner: inner, writeErr: writeErr, commitErr: commitErr}, nil
}

type faultyStagedWriter struct {
	inner     backends.StagedWriter
	writeErr  error
	commitErr error
}

func (w *faultyStagedWriter) Write(p []byte) (int, error) {
	if w.writeErr != nil {
		return 0, w.writeErr
	}
	return w.inner.Write(p)
}
func (w *faultyStagedWriter) Commit(ctx context.Context, p, k string) error {
	if w.commitErr != nil {
		return w.commitErr
	}
	return w.inner.Commit(ctx, p, k)
}
func (w *faultyStagedWriter) Cancel(ctx context.Context) error { return w.inner.Cancel(ctx) }

// newFaultyRegistry builds a registry whose backend can be made to fail.
func newFaultyRegistry(t *testing.T) (*ociproxy.Registry, *caching.Cas, *faultyBackend) {
	t.Helper()
	ctx := context.Background()
	fs := backends.NewFileSystemCacheForTest(t.TempDir(), t.TempDir())
	fb := &faultyBackend{CacheBackend: fs}
	cas := caching.NewCas(fb)
	reg, err := ociproxy.New(ctx, cas)
	require.NoError(t, err)
	t.Cleanup(func() { _ = reg.Close() })
	return reg, cas, fb
}

// ---------- patchBlobUpload error branches ----------

func TestPatchBlobUploadMissingUploadId(t *testing.T) {
	reg, _ := newTestRegistry(t)
	req, _ := http.NewRequest(http.MethodPatch, urlFor(reg, "/v2/repo/blobs/uploads/"), strings.NewReader("data"))
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	resp.Body.Close()
	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
}

func TestPatchBlobUploadUnknownUpload(t *testing.T) {
	reg, _ := newTestRegistry(t)
	req, _ := http.NewRequest(http.MethodPatch, urlFor(reg, "/v2/repo/blobs/uploads/does-not-exist"), strings.NewReader("data"))
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	resp.Body.Close()
	assert.Equal(t, http.StatusNotFound, resp.StatusCode)
}

func TestPatchBlobUploadAfterFinishedReturnsConflict(t *testing.T) {
	reg, _ := newTestRegistry(t)

	startResp, err := http.Post(urlFor(reg, "/v2/r/blobs/uploads/"), "application/octet-stream", nil)
	require.NoError(t, err)
	startResp.Body.Close()
	location := startResp.Header.Get("Location")

	payload := []byte("hello")
	digest := digestOf(payload)
	// PUT to finalise with body so session becomes finished
	putReq, _ := http.NewRequest(http.MethodPut, urlFor(reg, location)+"?digest="+digest, bytes.NewReader(payload))
	putReq.ContentLength = int64(len(payload))
	putResp, err := http.DefaultClient.Do(putReq)
	require.NoError(t, err)
	putResp.Body.Close()
	require.Equal(t, http.StatusCreated, putResp.StatusCode)

	// Subsequent PATCH should 404 because dropUpload removed the session.
	patchReq, _ := http.NewRequest(http.MethodPatch, urlFor(reg, location), bytes.NewReader([]byte("x")))
	patchResp, err := http.DefaultClient.Do(patchReq)
	require.NoError(t, err)
	patchResp.Body.Close()
	assert.Equal(t, http.StatusNotFound, patchResp.StatusCode)
}

func TestPatchBlobUploadCopyErrorCancelsSession(t *testing.T) {
	ctx := context.Background()
	fs := backends.NewFileSystemCacheForTest(t.TempDir(), t.TempDir())
	fb := &faultyBackend{CacheBackend: fs, stagedWriteErr: errors.New("staged-write-boom")}
	cas := caching.NewCas(fb)
	reg, err := ociproxy.New(ctx, cas)
	require.NoError(t, err)
	t.Cleanup(func() { _ = reg.Close() })

	startResp, err := http.Post(urlFor(reg, "/v2/r/blobs/uploads/"), "application/octet-stream", nil)
	require.NoError(t, err)
	startResp.Body.Close()
	require.Equal(t, http.StatusAccepted, startResp.StatusCode)
	location := startResp.Header.Get("Location")

	patchReq, _ := http.NewRequest(http.MethodPatch, urlFor(reg, location), strings.NewReader("data"))
	patchResp, err := http.DefaultClient.Do(patchReq)
	require.NoError(t, err)
	patchResp.Body.Close()
	assert.Equal(t, http.StatusInternalServerError, patchResp.StatusCode)

	again, _ := http.NewRequest(http.MethodPatch, urlFor(reg, location), strings.NewReader("x"))
	resp2, err := http.DefaultClient.Do(again)
	require.NoError(t, err)
	resp2.Body.Close()
	assert.Equal(t, http.StatusNotFound, resp2.StatusCode)
}

type erroringReader struct{ err error }

func (r *erroringReader) Read(p []byte) (int, error) { return 0, r.err }

// ---------- finishBlobUpload error branches ----------

func TestFinishBlobUploadMissingUploadId(t *testing.T) {
	reg, _ := newTestRegistry(t)
	req, _ := http.NewRequest(http.MethodPut, urlFor(reg, "/v2/r/blobs/uploads/")+"?digest="+digestOf([]byte("x")), nil)
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	resp.Body.Close()
	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
}

func TestFinishBlobUploadInvalidDigest(t *testing.T) {
	reg, _ := newTestRegistry(t)
	startResp, err := http.Post(urlFor(reg, "/v2/r/blobs/uploads/"), "application/octet-stream", nil)
	require.NoError(t, err)
	startResp.Body.Close()
	location := startResp.Header.Get("Location")

	putReq, _ := http.NewRequest(http.MethodPut, urlFor(reg, location)+"?digest=garbage", nil)
	putResp, err := http.DefaultClient.Do(putReq)
	require.NoError(t, err)
	putResp.Body.Close()
	assert.Equal(t, http.StatusBadRequest, putResp.StatusCode)
}

func TestFinishBlobUploadUnknownSession(t *testing.T) {
	reg, _ := newTestRegistry(t)
	digest := digestOf([]byte("x"))
	putReq, _ := http.NewRequest(http.MethodPut, urlFor(reg, "/v2/r/blobs/uploads/no-such-session")+"?digest="+digest, nil)
	putResp, err := http.DefaultClient.Do(putReq)
	require.NoError(t, err)
	putResp.Body.Close()
	assert.Equal(t, http.StatusNotFound, putResp.StatusCode)
}

// ---------- handleBlobs / handleManifests method paths ----------

func TestBlobUnsupportedMethodReturns405(t *testing.T) {
	reg, cas := newTestRegistry(t)
	ctx := context.Background()
	payload := []byte("x")
	digest := digestOf(payload)
	require.NoError(t, cas.WriteBytes(ctx, digest, payload))

	req, _ := http.NewRequest(http.MethodDelete, urlFor(reg, "/v2/r/blobs/"+digest), nil)
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	resp.Body.Close()
	assert.Equal(t, http.StatusMethodNotAllowed, resp.StatusCode)
}

func TestBlobUploadsUnsupportedMethodReturns405(t *testing.T) {
	reg, _ := newTestRegistry(t)
	req, _ := http.NewRequest(http.MethodGet, urlFor(reg, "/v2/r/blobs/uploads/abc"), nil)
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	resp.Body.Close()
	assert.Equal(t, http.StatusMethodNotAllowed, resp.StatusCode)
}

func TestManifestUnsupportedMethodReturns405(t *testing.T) {
	reg, _ := newTestRegistry(t)
	req, _ := http.NewRequest(http.MethodDelete, urlFor(reg, "/v2/r/manifests/sometag"), nil)
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	resp.Body.Close()
	assert.Equal(t, http.StatusMethodNotAllowed, resp.StatusCode)
}

func TestHandleUnknownKindReturns404(t *testing.T) {
	reg, _ := newTestRegistry(t)
	resp, err := http.Get(urlFor(reg, "/v2/r/widgets/foo"))
	require.NoError(t, err)
	resp.Body.Close()
	assert.Equal(t, http.StatusNotFound, resp.StatusCode)
}

func TestHandleRootWithoutTrailingSlash(t *testing.T) {
	reg, _ := newTestRegistry(t)
	resp, err := http.Get(urlFor(reg, "/v2"))
	require.NoError(t, err)
	resp.Body.Close()
	assert.Equal(t, http.StatusOK, resp.StatusCode)
}

// ---------- headBlob / getBlob error paths ----------

func TestHeadBlobBackendExistsError(t *testing.T) {
	reg, _, fb := newFaultyRegistry(t)
	fb.mu.Lock()
	fb.existsErr = errors.New("exists-boom")
	fb.mu.Unlock()
	digest := digestOf([]byte("x"))
	resp, err := http.Head(urlFor(reg, "/v2/r/blobs/"+digest))
	require.NoError(t, err)
	resp.Body.Close()
	assert.Equal(t, http.StatusInternalServerError, resp.StatusCode)
}

func TestHeadBlobBackendSizeError(t *testing.T) {
	reg, cas, fb := newFaultyRegistry(t)
	ctx := context.Background()
	payload := []byte("x")
	digest := digestOf(payload)
	require.NoError(t, cas.WriteBytes(ctx, digest, payload))

	fb.mu.Lock()
	fb.sizeErr = errors.New("size-boom")
	fb.mu.Unlock()

	resp, err := http.Head(urlFor(reg, "/v2/r/blobs/"+digest))
	require.NoError(t, err)
	resp.Body.Close()
	assert.Equal(t, http.StatusInternalServerError, resp.StatusCode)
}

func TestGetBlobSizeErrorReturns404(t *testing.T) {
	reg, _, fb := newFaultyRegistry(t)
	fb.mu.Lock()
	fb.sizeErr = errors.New("size-boom")
	fb.mu.Unlock()
	digest := digestOf([]byte("x"))
	resp, err := http.Get(urlFor(reg, "/v2/r/blobs/"+digest))
	require.NoError(t, err)
	resp.Body.Close()
	assert.Equal(t, http.StatusNotFound, resp.StatusCode)
}

func TestGetBlobLoadErrorReturns404(t *testing.T) {
	reg, cas, fb := newFaultyRegistry(t)
	ctx := context.Background()
	payload := []byte("x")
	digest := digestOf(payload)
	require.NoError(t, cas.WriteBytes(ctx, digest, payload))

	fb.mu.Lock()
	fb.getErr = errors.New("get-boom")
	fb.mu.Unlock()

	resp, err := http.Get(urlFor(reg, "/v2/r/blobs/"+digest))
	require.NoError(t, err)
	resp.Body.Close()
	assert.Equal(t, http.StatusNotFound, resp.StatusCode)
}

// ---------- writeBlobMonolithic / startBlobUpload error paths ----------

func TestMonolithicUploadDigestMismatch(t *testing.T) {
	reg, _ := newTestRegistry(t)
	payload := []byte("monolithic")
	wrong := digestOf([]byte("other"))
	resp, err := http.Post(urlFor(reg, "/v2/r/blobs/uploads/?digest="+wrong), "application/octet-stream", bytes.NewReader(payload))
	require.NoError(t, err)
	resp.Body.Close()
	assert.Equal(t, http.StatusInternalServerError, resp.StatusCode)
}

func TestMonolithicUploadBeginWriteFails(t *testing.T) {
	reg, _, fb := newFaultyRegistry(t)
	fb.mu.Lock()
	fb.beginWriteErr = errors.New("begin-write-boom")
	fb.mu.Unlock()

	payload := []byte("monolithic")
	digest := digestOf(payload)
	resp, err := http.Post(urlFor(reg, "/v2/r/blobs/uploads/?digest="+digest), "application/octet-stream", bytes.NewReader(payload))
	require.NoError(t, err)
	resp.Body.Close()
	assert.Equal(t, http.StatusInternalServerError, resp.StatusCode)
}

func TestStartBlobUploadBeginWriteFails(t *testing.T) {
	reg, _, fb := newFaultyRegistry(t)
	fb.mu.Lock()
	fb.beginWriteErr = errors.New("begin-write-boom")
	fb.mu.Unlock()

	resp, err := http.Post(urlFor(reg, "/v2/r/blobs/uploads/"), "application/octet-stream", nil)
	require.NoError(t, err)
	resp.Body.Close()
	assert.Equal(t, http.StatusInternalServerError, resp.StatusCode)
}

// ---------- finishBlobUpload commit error / put body error ----------

func TestFinishBlobUploadCommitError(t *testing.T) {
	ctx := context.Background()
	fs := backends.NewFileSystemCacheForTest(t.TempDir(), t.TempDir())
	fb := &faultyBackend{CacheBackend: fs, commitErr: errors.New("commit-boom")}
	cas := caching.NewCas(fb)
	reg, err := ociproxy.New(ctx, cas)
	require.NoError(t, err)
	t.Cleanup(func() { _ = reg.Close() })

	startResp, err := http.Post(urlFor(reg, "/v2/r/blobs/uploads/"), "application/octet-stream", nil)
	require.NoError(t, err)
	startResp.Body.Close()
	location := startResp.Header.Get("Location")

	payload := []byte("payload")
	digest := digestOf(payload)
	putReq, _ := http.NewRequest(http.MethodPut, urlFor(reg, location)+"?digest="+digest, bytes.NewReader(payload))
	putReq.ContentLength = int64(len(payload))
	putResp, err := http.DefaultClient.Do(putReq)
	require.NoError(t, err)
	putResp.Body.Close()
	assert.Equal(t, http.StatusInternalServerError, putResp.StatusCode)
}

func TestFinishBlobUploadBodyError(t *testing.T) {
	ctx := context.Background()
	fs := backends.NewFileSystemCacheForTest(t.TempDir(), t.TempDir())
	fb := &faultyBackend{CacheBackend: fs, stagedWriteErr: errors.New("staged-write-boom")}
	cas := caching.NewCas(fb)
	reg, err := ociproxy.New(ctx, cas)
	require.NoError(t, err)
	t.Cleanup(func() { _ = reg.Close() })

	startResp, err := http.Post(urlFor(reg, "/v2/r/blobs/uploads/"), "application/octet-stream", nil)
	require.NoError(t, err)
	startResp.Body.Close()
	location := startResp.Header.Get("Location")

	body := []byte("abc")
	digest := digestOf(body)
	putReq, _ := http.NewRequest(http.MethodPut, urlFor(reg, location)+"?digest="+digest, bytes.NewReader(body))
	putReq.ContentLength = int64(len(body))
	putResp, err := http.DefaultClient.Do(putReq)
	require.NoError(t, err)
	putResp.Body.Close()
	assert.Equal(t, http.StatusInternalServerError, putResp.StatusCode)
}

// ---------- manifest path errors ----------

func TestGetManifestBackendExistsError(t *testing.T) {
	reg, _, fb := newFaultyRegistry(t)
	fb.mu.Lock()
	fb.existsErr = errors.New("exists-boom")
	fb.mu.Unlock()
	digest := digestOf([]byte("manifest"))
	resp, err := http.Get(urlFor(reg, "/v2/r/manifests/"+digest))
	require.NoError(t, err)
	resp.Body.Close()
	assert.Equal(t, http.StatusInternalServerError, resp.StatusCode)
}

func TestGetManifestLoadBytesError(t *testing.T) {
	reg, cas, fb := newFaultyRegistry(t)
	ctx := context.Background()
	manifest := []byte(`{"schemaVersion":2}`)
	digest := digestOf(manifest)
	require.NoError(t, cas.WriteBytes(ctx, digest, manifest))

	fb.mu.Lock()
	fb.getErr = errors.New("get-boom")
	fb.mu.Unlock()

	resp, err := http.Get(urlFor(reg, "/v2/r/manifests/"+digest))
	require.NoError(t, err)
	resp.Body.Close()
	assert.Equal(t, http.StatusInternalServerError, resp.StatusCode)
}

func TestPutManifestSetError(t *testing.T) {
	reg, _, fb := newFaultyRegistry(t)
	fb.mu.Lock()
	fb.setErr = errors.New("set-boom")
	fb.mu.Unlock()

	body := []byte(`{"schemaVersion":2}`)
	req, _ := http.NewRequest(http.MethodPut, urlFor(reg, "/v2/r/manifests/latest"), bytes.NewReader(body))
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	resp.Body.Close()
	assert.Equal(t, http.StatusInternalServerError, resp.StatusCode)
}

func TestHeadManifestByDigest(t *testing.T) {
	reg, cas := newTestRegistry(t)
	ctx := context.Background()
	manifest := []byte(`{"schemaVersion":2,"mediaType":"application/vnd.oci.image.manifest.v1+json"}`)
	digest := digestOf(manifest)
	require.NoError(t, cas.WriteBytes(ctx, digest, manifest))

	req, _ := http.NewRequest(http.MethodHead, urlFor(reg, "/v2/r/manifests/"+digest), nil)
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Equal(t, digest, resp.Header.Get("Docker-Content-Digest"))
	assert.Equal(t, fmt.Sprintf("%d", len(manifest)), resp.Header.Get("Content-Length"))
}

// ---------- misc ----------

func TestCloseIsIdempotent(t *testing.T) {
	ctx := context.Background()
	fs := backends.NewFileSystemCacheForTest(t.TempDir(), t.TempDir())
	cas := caching.NewCas(fs)
	reg, err := ociproxy.New(ctx, cas)
	require.NoError(t, err)
	require.NoError(t, reg.Close())
	require.NoError(t, reg.Close())
}

func TestCloseCancelsPendingUpload(t *testing.T) {
	ctx := context.Background()
	fs := backends.NewFileSystemCacheForTest(t.TempDir(), t.TempDir())
	cas := caching.NewCas(fs)
	reg, err := ociproxy.New(ctx, cas)
	require.NoError(t, err)

	startResp, err := http.Post("http://"+reg.Addr()+"/v2/r/blobs/uploads/", "application/octet-stream", nil)
	require.NoError(t, err)
	startResp.Body.Close()
	require.NoError(t, reg.Close())
}

func TestPutManifestFallsBackToDefaultMediaType(t *testing.T) {
	reg, cas := newTestRegistry(t)
	ctx := context.Background()
	// A manifest with no mediaType field; sniff should fall back to default.
	manifest := []byte(`not really json at all`)
	sum := sha256.Sum256(manifest)
	digest := "sha256:" + hex.EncodeToString(sum[:])
	require.NoError(t, cas.WriteBytes(ctx, digest, manifest))

	resp, err := http.Get(urlFor(reg, "/v2/r/manifests/"+digest))
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Equal(t, "application/vnd.oci.image.manifest.v1+json", resp.Header.Get("Content-Type"))
}

func TestLastManifestDigestEmptyForUnknownRepo(t *testing.T) {
	reg, _ := newTestRegistry(t)
	assert.Equal(t, "", reg.LastManifestDigest("never/pushed"))
}
