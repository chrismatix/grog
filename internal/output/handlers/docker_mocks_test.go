package handlers

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"sync"

	"github.com/docker/docker/api/types/image"
	"github.com/docker/docker/client"
)

// mockDockerClient implements DockerClient with in-memory state for tests.
type mockDockerClient struct {
	mu sync.Mutex

	// images maps a ref or image ID to its known InspectResponse.
	images map[string]image.InspectResponse
	// tags maps a tag to the image ID it points at.
	tags map[string]string

	// pushStream is the JSON payload returned from ImagePush.
	pushStream string
	// pullStream is the JSON payload returned from ImagePull.
	pullStream string

	// errInspect causes ImageInspect to return this error if non-nil.
	errInspect error
	errTag     error
	errPush    error
	errPull    error
	errRemove  error

	// missingOnRemove makes ImageRemove return a not-found error.
	missingOnRemove bool
}

func newMockDockerClient() *mockDockerClient {
	return &mockDockerClient{
		images: map[string]image.InspectResponse{},
		tags:   map[string]string{},
	}
}

func (m *mockDockerClient) ImageInspect(_ context.Context, ref string, _ ...client.ImageInspectOption) (image.InspectResponse, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.errInspect != nil {
		return image.InspectResponse{}, m.errInspect
	}
	if id, ok := m.tags[ref]; ok {
		ref = id
	}
	if img, ok := m.images[ref]; ok {
		return img, nil
	}
	return image.InspectResponse{}, errors.New("no such image: " + ref)
}

func (m *mockDockerClient) ImageTag(_ context.Context, src, target string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.errTag != nil {
		return m.errTag
	}
	id, ok := m.tags[src]
	if !ok {
		if _, idOk := m.images[src]; idOk {
			id = src
		} else {
			return errors.New("no such image: " + src)
		}
	}
	m.tags[target] = id
	return nil
}

func (m *mockDockerClient) ImagePush(_ context.Context, _ string, _ image.PushOptions) (io.ReadCloser, error) {
	if m.errPush != nil {
		return nil, m.errPush
	}
	return io.NopCloser(strings.NewReader(m.pushStream)), nil
}

func (m *mockDockerClient) ImagePull(_ context.Context, _ string, _ image.PullOptions) (io.ReadCloser, error) {
	if m.errPull != nil {
		return nil, m.errPull
	}
	return io.NopCloser(strings.NewReader(m.pullStream)), nil
}

func (m *mockDockerClient) ImageRemove(_ context.Context, ref string, _ image.RemoveOptions) ([]image.DeleteResponse, error) {
	if m.errRemove != nil {
		return nil, m.errRemove
	}
	if m.missingOnRemove {
		return nil, &notFoundError{ref: ref}
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.tags, ref)
	return nil, nil
}

// notFoundError mimics containerd/errdefs not-found semantics — wraps anything
// the errdefs.IsNotFound check can match against. The handler treats this as
// a soft warning and continues.
type notFoundError struct {
	ref string
}

func (e *notFoundError) Error() string { return "not found: " + e.ref }

// NotFound is the marker method errdefs.IsNotFound looks for.
func (e *notFoundError) NotFound() {}

// mockLoopbackRegistry is a stand-in for *ociproxy.Registry.
type mockLoopbackRegistry struct {
	addr           string
	manifestByName map[string]string
	closeErr       error
	closed         bool
}

func newMockLoopbackRegistry() *mockLoopbackRegistry {
	return &mockLoopbackRegistry{
		addr:           "127.0.0.1:0",
		manifestByName: map[string]string{},
	}
}

func (m *mockLoopbackRegistry) Addr() string { return m.addr }
func (m *mockLoopbackRegistry) LastManifestDigest(repoName string) string {
	return m.manifestByName[repoName]
}
func (m *mockLoopbackRegistry) Close() error {
	m.closed = true
	return m.closeErr
}

// progressJSONLayered builds a Docker JSON-message stream suitable for
// driving consumeDockerProgress with a layer in Pushing state followed by
// Pushed completion. Lets tests assert push/pull plumbing exercises the
// progress reader.
func progressJSONLayered() string {
	var buf bytes.Buffer
	buf.WriteString(`{"id":"l1","status":"Preparing"}` + "\n")
	buf.WriteString(`{"id":"l1","status":"Pushing","progressDetail":{"current":50,"total":100}}` + "\n")
	buf.WriteString(`{"id":"l1","status":"Pushing","progressDetail":{"current":100,"total":100}}` + "\n")
	buf.WriteString(`{"id":"l1","status":"Pushed"}` + "\n")
	return buf.String()
}
