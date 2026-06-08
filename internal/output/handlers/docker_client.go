package handlers

import (
	"context"
	"io"

	"github.com/docker/docker/api/types/image"
	"github.com/docker/docker/client"
)

// DockerClient is the subset of github.com/docker/docker/client.Client used by
// the Docker output handlers. Wrapping the client behind an interface lets the
// handler logic be unit-tested against a mock without spinning up a real
// Docker daemon. *client.Client already satisfies this interface, so no
// adapter wrapping is required at runtime.
type DockerClient interface {
	ImageInspect(ctx context.Context, ref string, opts ...client.ImageInspectOption) (image.InspectResponse, error)
	ImageTag(ctx context.Context, src, target string) error
	ImagePush(ctx context.Context, ref string, opts image.PushOptions) (io.ReadCloser, error)
	ImagePull(ctx context.Context, ref string, opts image.PullOptions) (io.ReadCloser, error)
	ImageRemove(ctx context.Context, ref string, opts image.RemoveOptions) ([]image.DeleteResponse, error)
}

// LoopbackRegistry is the subset of ociproxy.Registry used by the in-process
// Docker output handler. Same rationale as DockerClient: tests can inject a
// mock without standing up the real loopback HTTP listener.
type LoopbackRegistry interface {
	Addr() string
	LastManifestDigest(repoName string) string
	Close() error
}
