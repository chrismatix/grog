package oci_push_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/go-containerregistry/pkg/registry"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"grog/internal/caching"
	"grog/internal/caching/backends"
	"grog/internal/oci_push"
	"grog/internal/ociproxy"
)

func newDestinationRegistry(t *testing.T) (*httptest.Server, string) {
	t.Helper()
	server := httptest.NewServer(registry.New(registry.Logger(log.New(io.Discard, "", 0))))
	t.Cleanup(server.Close)
	return server, strings.TrimPrefix(server.URL, "http://")
}

func digestOf(payload []byte) string {
	sum := sha256.Sum256(payload)
	return "sha256:" + hex.EncodeToString(sum[:])
}

func insecureAlways(string) bool { return true }

func TestTeeStreamsBlobToAllDestinationRepos(t *testing.T) {
	server, host := newDestinationRegistry(t)
	ctx := context.Background()

	// Two tags on the same repo must dedupe into one upload; the second repo
	// gets its own.
	tee, err := oci_push.NewTee(ctx, []string{
		host + "/test/app:v1",
		host + "/test/app:v2",
		host + "/other/app:v1",
	}, insecureAlways)
	require.NoError(t, err)

	payload := []byte("fake compressed layer bytes")
	digest := digestOf(payload)

	sink := tee.OpenBlob()
	require.NotNil(t, sink)
	for _, chunk := range [][]byte{payload[:9], payload[9:]} {
		written, err := sink.Write(chunk)
		require.NoError(t, err)
		require.Equal(t, len(chunk), written)
	}
	sink.Commit(digest)

	uploadCount, failures := tee.Wait()
	assert.Empty(t, failures)
	assert.Equal(t, 2, uploadCount, "one upload per deduplicated destination repo")

	for _, repo := range []string{"test/app", "other/app"} {
		resp, err := http.Get(server.URL + "/v2/" + repo + "/blobs/" + digest)
		require.NoError(t, err)
		body, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		require.NoError(t, err)
		assert.Equal(t, http.StatusOK, resp.StatusCode, repo)
		assert.Equal(t, payload, body, repo)
	}
}

func TestTeeCancelDropsBlob(t *testing.T) {
	server, host := newDestinationRegistry(t)
	ctx := context.Background()

	tee, err := oci_push.NewTee(ctx, []string{host + "/test/app:v1"}, insecureAlways)
	require.NoError(t, err)

	payload := []byte("bytes that never complete")

	sink := tee.OpenBlob()
	require.NotNil(t, sink)
	_, _ = sink.Write(payload)
	sink.Cancel()

	uploadCount, _ := tee.Wait()
	assert.Zero(t, uploadCount)

	resp, err := http.Get(server.URL + "/v2/test/app/blobs/" + digestOf(payload))
	require.NoError(t, err)
	resp.Body.Close()
	assert.Equal(t, http.StatusNotFound, resp.StatusCode)
}

func TestTeeRecordsFailureAndStopsOpeningUploads(t *testing.T) {
	server, host := newDestinationRegistry(t)
	server.Close() // destination is dead from the start
	ctx := context.Background()

	tee, err := oci_push.NewTee(ctx, []string{host + "/test/app:v1"}, insecureAlways)
	require.NoError(t, err)

	payload := []byte("payload for a dead registry")
	sink := tee.OpenBlob()
	require.NotNil(t, sink)
	_, _ = sink.Write(payload)
	sink.Commit(digestOf(payload))

	uploadCount, failures := tee.Wait()
	assert.Zero(t, uploadCount)
	assert.Len(t, failures, 1)

	// The failed repo is skipped for subsequent blobs.
	assert.Nil(t, tee.OpenBlob())
}

func TestTeeAbortUnblocksWait(t *testing.T) {
	hung := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	}))
	t.Cleanup(hung.Close)
	host := strings.TrimPrefix(hung.URL, "http://")
	ctx := context.Background()

	tee, err := oci_push.NewTee(ctx, []string{host + "/test/app:v1"}, insecureAlways)
	require.NoError(t, err)

	sink := tee.OpenBlob()
	require.NotNil(t, sink)

	tee.Abort()
	_, failures := tee.Wait() // must not deadlock
	assert.Len(t, failures, 1)
}

func TestTeeRejectsUnparsableDestination(t *testing.T) {
	_, err := oci_push.NewTee(context.Background(), []string{"registry.example/UPPER CASE"}, insecureAlways)
	require.Error(t, err)
}

// TestProxyUploadStreamsToDestinationWhileCaching wires the real proxy to a
// real Tee: a chunked upload (as the docker daemon would send it) must land in
// the CAS and, concurrently, in the destination registry.
func TestProxyUploadStreamsToDestinationWhileCaching(t *testing.T) {
	server, host := newDestinationRegistry(t)
	ctx := context.Background()

	cas := caching.NewCas(backends.NewFileSystemCacheForTest(t.TempDir(), t.TempDir()))
	proxy, err := ociproxy.New(ctx, cas)
	require.NoError(t, err)
	t.Cleanup(func() { _ = proxy.Close() })

	tee, err := oci_push.NewTee(ctx, []string{host + "/test/app:v1"}, insecureAlways)
	require.NoError(t, err)
	proxy.SetTee("grog-cache/abc", tee)

	payload := bytes.Repeat([]byte("layer-bytes-"), 4096)
	digest := digestOf(payload)

	startResp, err := http.Post("http://"+proxy.Addr()+"/v2/grog-cache/abc/blobs/uploads/", "application/octet-stream", nil)
	require.NoError(t, err)
	startResp.Body.Close()
	location := startResp.Header.Get("Location")

	patchReq, err := http.NewRequest(http.MethodPatch, "http://"+proxy.Addr()+location, bytes.NewReader(payload))
	require.NoError(t, err)
	patchResp, err := http.DefaultClient.Do(patchReq)
	require.NoError(t, err)
	patchResp.Body.Close()
	require.Equal(t, http.StatusAccepted, patchResp.StatusCode)

	putReq, err := http.NewRequest(http.MethodPut, "http://"+proxy.Addr()+location+"?digest="+digest, nil)
	require.NoError(t, err)
	putResp, err := http.DefaultClient.Do(putReq)
	require.NoError(t, err)
	putResp.Body.Close()
	require.Equal(t, http.StatusCreated, putResp.StatusCode)

	proxy.ClearTee("grog-cache/abc")
	uploadCount, failures := tee.Wait()
	assert.Empty(t, failures)
	assert.Equal(t, 1, uploadCount)

	cached, err := cas.LoadBytes(ctx, digest)
	require.NoError(t, err)
	assert.Equal(t, payload, cached)

	resp, err := http.Get(server.URL + "/v2/test/app/blobs/" + digest)
	require.NoError(t, err)
	pushed, err := io.ReadAll(resp.Body)
	resp.Body.Close()
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Equal(t, payload, pushed)
}
