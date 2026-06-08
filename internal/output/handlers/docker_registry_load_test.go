package handlers

import (
	"context"
	"errors"
	"testing"

	"github.com/docker/docker/api/types/image"

	"grog/internal/config"
	"grog/internal/label"
	"grog/internal/model"
	"grog/internal/proto/gen"
)

func TestDockerRegistryOutputHandler_Load_PullPath(t *testing.T) {
	prev := config.Global
	config.Global = config.WorkspaceConfig{WorkspaceRoot: "/tmp/abc"}
	t.Cleanup(func() { config.Global = prev })

	mock := newMockDockerClient()
	mock.pullStream = progressJSONLayered()
	// Inspect on image ID misses → fall through to pull path.
	mock.errInspect = errors.New("not present")
	// After pull the handler tags the remote ref as the local name. Mock requires
	// the tag source to exist as an image — preload it.
	mock.images["gcr.io/x/y"] = image.InspectResponse{ID: "gcr.io/x/y"}
	mock.errInspect = nil
	delete(mock.images, "sha256:abc")
	mock.errInspect = errors.New("not present")

	h := NewDockerRegistryOutputHandlerWithClient(nil, config.OCIConfig{Registry: "gcr.io/x"}, mock)
	out := &gen.Output{Kind: &gen.Output_OciImage{
		OciImage: &gen.OCIImageOutput{
			Mode:      gen.ImageMode_REGISTRY,
			LocalTag:  "myimg",
			ImageId:   "sha256:abc",
			RemoteTag: "gcr.io/x/y",
		},
	}}
	if err := h.Load(context.Background(), model.Target{Label: label.TL("p", "t")}, out, nil); err != nil {
		t.Fatalf("Load: %v", err)
	}
}

func TestDockerRegistryOutputHandler_Load_PullFails(t *testing.T) {
	prev := config.Global
	config.Global = config.WorkspaceConfig{WorkspaceRoot: "/tmp/abc"}
	t.Cleanup(func() { config.Global = prev })

	mock := newMockDockerClient()
	mock.errInspect = errors.New("not present")
	mock.errPull = errors.New("pull denied")

	h := NewDockerRegistryOutputHandlerWithClient(nil, config.OCIConfig{Registry: "gcr.io/x"}, mock)
	out := &gen.Output{Kind: &gen.Output_OciImage{
		OciImage: &gen.OCIImageOutput{
			Mode:      gen.ImageMode_REGISTRY,
			LocalTag:  "myimg",
			ImageId:   "sha256:abc",
			RemoteTag: "gcr.io/x/y",
		},
	}}
	if err := h.Load(context.Background(), model.Target{Label: label.TL("p", "t")}, out, nil); err == nil {
		t.Fatal("expected err")
	}
}

func TestDockerRegistryOutputHandler_Load_FastPath_TagFails(t *testing.T) {
	mock := newMockDockerClient()
	mock.images["sha256:abc"] = image.InspectResponse{ID: "sha256:abc"}
	mock.errTag = errors.New("tag denied")

	h := NewDockerRegistryOutputHandlerWithClient(nil, config.OCIConfig{}, mock)
	out := &gen.Output{Kind: &gen.Output_OciImage{
		OciImage: &gen.OCIImageOutput{
			Mode:     gen.ImageMode_REGISTRY,
			LocalTag: "myimg",
			ImageId:  "sha256:abc",
		},
	}}
	if err := h.Load(context.Background(), model.Target{Label: label.TL("p", "t")}, out, nil); err == nil {
		t.Fatal("expected err")
	}
}

func TestDockerOutputHandler_Write_NoProxyOrClient(t *testing.T) {
	// Default constructor lazily creates the docker client. Without a daemon
	// available, ensureClient should error.
	h := NewDockerOutputHandler(context.Background(), nil)
	_, err := h.Write(context.Background(), model.Target{Label: label.TL("p", "t")}, model.NewOutput("oci", "x"), nil)
	if err == nil {
		t.Skip("docker daemon is available; constructor lazy path succeeded")
	}
}
