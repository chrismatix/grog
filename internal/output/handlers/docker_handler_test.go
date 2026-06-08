package handlers

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/docker/docker/api/types/image"

	"grog/internal/config"
	"grog/internal/label"
	"grog/internal/model"
	"grog/internal/proto/gen"
	"grog/internal/worker"
)

func TestDockerOutputHandler_Hash(t *testing.T) {
	mock := newMockDockerClient()
	mock.images["myimg:latest"] = image.InspectResponse{ID: "sha256:abc123"}

	h := NewDockerOutputHandlerWithClient(nil, mock, newMockLoopbackRegistry())
	hash, err := h.Hash(context.Background(), model.Target{Label: label.TL("pkg", "t")}, model.NewOutput("oci", "myimg:latest"))
	if err != nil {
		t.Fatalf("Hash: %v", err)
	}
	if hash != "sha256:abc123" {
		t.Fatalf("got %q", hash)
	}
}

func TestDockerOutputHandler_Hash_InspectFails(t *testing.T) {
	mock := newMockDockerClient()
	mock.errInspect = errors.New("boom")

	h := NewDockerOutputHandlerWithClient(nil, mock, newMockLoopbackRegistry())
	if _, err := h.Hash(context.Background(), model.Target{Label: label.TL("p", "t")}, model.NewOutput("oci", "x")); err == nil {
		t.Fatal("expected err")
	}
}

func TestDockerOutputHandler_Write(t *testing.T) {
	mock := newMockDockerClient()
	mock.images["myimg:latest"] = image.InspectResponse{ID: "sha256:deadbeefdeadbeef"}
	reg := newMockLoopbackRegistry()
	h := NewDockerOutputHandlerWithClient(nil, mock, reg)

	tgt := model.Target{Label: label.TL("pkg", "t")}
	out := model.NewOutput("oci", "myimg:latest")
	prepared, err := h.Write(context.Background(), tgt, out, nil)
	if err != nil {
		t.Fatalf("Write: %v", err)
	}
	if prepared == nil || prepared.WritePlan == nil {
		t.Fatal("missing plan")
	}
	if prepared.Output.GetOciImage().GetLocalTag() != "myimg:latest" {
		t.Fatalf("got %v", prepared.Output)
	}
	if prepared.Output.GetOciImage().GetMode() != gen.ImageMode_LAYERS {
		t.Fatal("expected LAYERS mode")
	}
}

func TestDockerOutputHandler_Write_TagFails(t *testing.T) {
	mock := newMockDockerClient()
	mock.images["myimg:latest"] = image.InspectResponse{ID: "sha256:deadbeef"}
	mock.errTag = errors.New("tag failed")
	h := NewDockerOutputHandlerWithClient(nil, mock, newMockLoopbackRegistry())

	if _, err := h.Write(context.Background(), model.Target{Label: label.TL("p", "t")}, model.NewOutput("oci", "myimg:latest"), nil); err == nil {
		t.Fatal("expected err")
	}
}

func TestDockerOutputHandler_Write_InspectFails(t *testing.T) {
	mock := newMockDockerClient()
	mock.errInspect = errors.New("not created")
	h := NewDockerOutputHandlerWithClient(nil, mock, newMockLoopbackRegistry())
	if _, err := h.Write(context.Background(), model.Target{Label: label.TL("p", "t")}, model.NewOutput("oci", "x"), nil); err == nil {
		t.Fatal("expected err")
	}
}

func TestDockerImageWritePlan_Execute(t *testing.T) {
	mock := newMockDockerClient()
	mock.pushStream = progressJSONLayered()
	reg := newMockLoopbackRegistry()
	reg.manifestByName["grog-cache/abc"] = "sha256:manifestdigest"

	out := &gen.OCIImageOutput{}
	plan := &dockerImageWritePlan{
		dockerClient:   mock,
		proxy:          reg,
		output:         out,
		loopbackRef:    "127.0.0.1:0/grog-cache/abc:abc",
		repoName:       "grog-cache/abc",
		localImageName: "myimg",
		targetLabel:    "//pkg:t",
	}
	tracker := worker.NewProgressTracker("status", 0, nil)
	if err := plan.Execute(context.Background(), tracker); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if out.GetManifestDigest().GetHash() != "sha256:manifestdigest" {
		t.Fatalf("digest not stored: %v", out.GetManifestDigest())
	}
}

func TestDockerImageWritePlan_Execute_NoManifest(t *testing.T) {
	mock := newMockDockerClient()
	mock.pushStream = progressJSONLayered()
	reg := newMockLoopbackRegistry()
	plan := &dockerImageWritePlan{
		dockerClient: mock,
		proxy:        reg,
		output:       &gen.OCIImageOutput{},
		loopbackRef:  "x", repoName: "grog-cache/abc", localImageName: "i", targetLabel: "//p:t",
	}
	if err := plan.Execute(context.Background(), worker.NewProgressTracker("s", 0, nil)); err == nil {
		t.Fatal("expected err — registry never received a manifest")
	}
}

func TestDockerImageWritePlan_Execute_PushFails(t *testing.T) {
	mock := newMockDockerClient()
	mock.errPush = errors.New("push refused")
	plan := &dockerImageWritePlan{
		dockerClient: mock,
		proxy:        newMockLoopbackRegistry(),
		output:       &gen.OCIImageOutput{},
		loopbackRef:  "x", repoName: "r", localImageName: "i", targetLabel: "//p:t",
	}
	if err := plan.Execute(context.Background(), worker.NewProgressTracker("s", 0, nil)); err == nil {
		t.Fatal("expected err")
	}
}

func TestDockerImageWritePlan_Cleanup(t *testing.T) {
	mock := newMockDockerClient()
	plan := &dockerImageWritePlan{dockerClient: mock, loopbackRef: "x"}
	if err := plan.Cleanup(context.Background()); err != nil {
		t.Fatal(err)
	}

	mock.missingOnRemove = true
	if err := plan.Cleanup(context.Background()); err != nil {
		t.Fatal("not-found should be tolerated")
	}
}

func TestDockerOutputHandler_Close_WithProxy(t *testing.T) {
	reg := newMockLoopbackRegistry()
	h := NewDockerOutputHandlerWithClient(nil, newMockDockerClient(), reg)
	if err := h.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if !reg.closed {
		t.Fatal("registry not closed")
	}
}

func TestDockerOutputHandler_Load_FastPath(t *testing.T) {
	mock := newMockDockerClient()
	mock.images["sha256:abc"] = image.InspectResponse{ID: "sha256:abc"}
	h := NewDockerOutputHandlerWithClient(nil, mock, newMockLoopbackRegistry())

	out := &gen.Output{Kind: &gen.Output_OciImage{
		OciImage: &gen.OCIImageOutput{
			Mode:           gen.ImageMode_LAYERS,
			LocalTag:       "myimg:latest",
			ImageId:        "sha256:abc",
			ManifestDigest: &gen.Digest{Hash: "sha256:manifest"},
		},
	}}
	if err := h.Load(context.Background(), model.Target{Label: label.TL("p", "t")}, out, nil); err != nil {
		t.Fatalf("Load: %v", err)
	}
}

func TestDockerOutputHandler_Load_WrongMode(t *testing.T) {
	mock := newMockDockerClient()
	h := NewDockerOutputHandlerWithClient(nil, mock, newMockLoopbackRegistry())

	out := &gen.Output{Kind: &gen.Output_OciImage{
		OciImage: &gen.OCIImageOutput{Mode: gen.ImageMode_REGISTRY},
	}}
	if err := h.Load(context.Background(), model.Target{Label: label.TL("p", "t")}, out, nil); err == nil {
		t.Fatal("expected err")
	}
}

func TestDockerOutputHandler_Load_MissingDigest(t *testing.T) {
	h := NewDockerOutputHandlerWithClient(nil, newMockDockerClient(), newMockLoopbackRegistry())
	out := &gen.Output{Kind: &gen.Output_OciImage{
		OciImage: &gen.OCIImageOutput{Mode: gen.ImageMode_LAYERS, LocalTag: "x"},
	}}
	if err := h.Load(context.Background(), model.Target{Label: label.TL("p", "t")}, out, nil); err == nil {
		t.Fatal("expected err")
	}
}

func TestDockerOutputHandler_Load_PullPath(t *testing.T) {
	mock := newMockDockerClient()
	mock.pullStream = progressJSONLayered()
	mock.errInspect = errors.New("not present")
	h := NewDockerOutputHandlerWithClient(nil, mock, newMockLoopbackRegistry())

	out := &gen.Output{Kind: &gen.Output_OciImage{
		OciImage: &gen.OCIImageOutput{
			Mode:           gen.ImageMode_LAYERS,
			LocalTag:       "myimg:latest",
			ImageId:        "sha256:abc",
			ManifestDigest: &gen.Digest{Hash: "sha256:manifest"},
		},
	}}
	// pull path tries Inspect then Pull; tags after pull; clears tag at end.
	// With errInspect set every Inspect fails, but Tag now needs an image —
	// preload it.
	mock.errInspect = nil
	mock.images["sha256:abc"] = image.InspectResponse{}
	delete(mock.images, "sha256:abc") // ensure inspect misses fast path
	mock.errInspect = errors.New("not in store")
	mock.errTag = nil
	// Tagging the synthetic pullRef requires the ref exists; the mock's
	// ImageTag falls back to assuming the src is itself an image id, so
	// preload that mapping by treating pullRef as an existing image id.
	pullRef := "127.0.0.1:0/grog-cache/abc@sha256:manifest"
	mock.images[pullRef] = image.InspectResponse{ID: pullRef}

	if err := h.Load(context.Background(), model.Target{Label: label.TL("p", "t")}, out, nil); err != nil {
		t.Fatalf("Load: %v", err)
	}
}

func TestDockerRegistryOutputHandler_Hash(t *testing.T) {
	mock := newMockDockerClient()
	mock.images["myimg"] = image.InspectResponse{ID: "sha256:abc"}
	h := NewDockerRegistryOutputHandlerWithClient(nil, config.OCIConfig{Registry: "gcr.io/x"}, mock)
	hash, err := h.Hash(context.Background(), model.Target{Label: label.TL("p", "t")}, model.NewOutput("oci", "myimg"))
	if err != nil {
		t.Fatalf("Hash: %v", err)
	}
	if hash != "sha256:abc" {
		t.Fatalf("got %q", hash)
	}
}

func TestDockerRegistryOutputHandler_Hash_InspectFails(t *testing.T) {
	mock := newMockDockerClient()
	mock.errInspect = errors.New("boom")
	h := NewDockerRegistryOutputHandlerWithClient(nil, config.OCIConfig{}, mock)
	if _, err := h.Hash(context.Background(), model.Target{Label: label.TL("p", "t")}, model.NewOutput("oci", "x")); err == nil {
		t.Fatal("expected err")
	}
}

func TestDockerRegistryOutputHandler_Write(t *testing.T) {
	prev := config.Global
	config.Global = config.WorkspaceConfig{WorkspaceRoot: "/tmp/abc"}
	t.Cleanup(func() { config.Global = prev })

	mock := newMockDockerClient()
	mock.images["myimg"] = image.InspectResponse{ID: "sha256:deadbeef"}
	h := NewDockerRegistryOutputHandlerWithClient(nil, config.OCIConfig{Registry: "gcr.io/x"}, mock)
	prepared, err := h.Write(context.Background(), model.Target{Label: label.TL("p", "t")}, model.NewOutput("oci", "myimg"), nil)
	if err != nil {
		t.Fatalf("Write: %v", err)
	}
	if prepared == nil || prepared.WritePlan == nil {
		t.Fatal("missing plan")
	}
	if !strings.Contains(prepared.Output.GetOciImage().GetRemoteTag(), "gcr.io/x") {
		t.Fatalf("got %q", prepared.Output.GetOciImage().GetRemoteTag())
	}
}

func TestDockerRegistryOutputHandler_Write_TagFails(t *testing.T) {
	prev := config.Global
	config.Global = config.WorkspaceConfig{WorkspaceRoot: "/tmp/abc"}
	t.Cleanup(func() { config.Global = prev })

	mock := newMockDockerClient()
	mock.images["myimg"] = image.InspectResponse{ID: "sha256:deadbeef"}
	mock.errTag = errors.New("tag failed")
	h := NewDockerRegistryOutputHandlerWithClient(nil, config.OCIConfig{Registry: "gcr.io/x"}, mock)
	if _, err := h.Write(context.Background(), model.Target{Label: label.TL("p", "t")}, model.NewOutput("oci", "myimg"), nil); err == nil {
		t.Fatal("expected err")
	}
}

func TestDockerRegistryWritePlan_Execute(t *testing.T) {
	prev := config.Global
	config.Global = config.WorkspaceConfig{WorkspaceRoot: "/tmp/abc"}
	t.Cleanup(func() { config.Global = prev })

	mock := newMockDockerClient()
	mock.pushStream = progressJSONLayered()
	plan := &dockerRegistryWritePlan{
		dockerClient:    mock,
		remoteImageName: "gcr.io/x/img",
		localImageName:  "img",
		targetLabel:     "//p:t",
	}
	tracker := worker.NewProgressTracker("s", 0, nil)
	if err := plan.Execute(context.Background(), tracker); err != nil {
		t.Fatalf("Execute: %v", err)
	}
}

func TestDockerRegistryWritePlan_Execute_PushFails(t *testing.T) {
	mock := newMockDockerClient()
	mock.errPush = errors.New("push refused")
	plan := &dockerRegistryWritePlan{dockerClient: mock, remoteImageName: "gcr.io/x/img"}
	if err := plan.Execute(context.Background(), worker.NewProgressTracker("s", 0, nil)); err == nil {
		t.Fatal("expected err")
	}
}

func TestDockerRegistryWritePlan_Cleanup(t *testing.T) {
	mock := newMockDockerClient()
	plan := &dockerRegistryWritePlan{dockerClient: mock, remoteImageName: "gcr.io/x/img"}
	if err := plan.Cleanup(context.Background()); err != nil {
		t.Fatal(err)
	}

	mock.missingOnRemove = true
	if err := plan.Cleanup(context.Background()); err != nil {
		t.Fatal("not-found should be tolerated")
	}
}

func TestDockerRegistryOutputHandler_Load_FastPath(t *testing.T) {
	mock := newMockDockerClient()
	mock.images["sha256:abc"] = image.InspectResponse{ID: "sha256:abc"}
	h := NewDockerRegistryOutputHandlerWithClient(nil, config.OCIConfig{}, mock)
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

func TestDockerRegistryOutputHandler_Load_WrongMode(t *testing.T) {
	mock := newMockDockerClient()
	h := NewDockerRegistryOutputHandlerWithClient(nil, config.OCIConfig{}, mock)
	out := &gen.Output{Kind: &gen.Output_OciImage{
		OciImage: &gen.OCIImageOutput{Mode: gen.ImageMode_LAYERS},
	}}
	if err := h.Load(context.Background(), model.Target{Label: label.TL("p", "t")}, out, nil); err == nil {
		t.Fatal("expected err")
	}
}
