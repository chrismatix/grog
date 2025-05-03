package handlers

import (
	"context"
	"errors"
	"fmt"
	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/google/go-containerregistry/pkg/v1/remote/transport"
	"grog/internal/caching"
	"net/http"

	"grog/internal/config"
	"grog/internal/console"
	"grog/internal/model"

	"github.com/google/go-containerregistry/pkg/name"
	"github.com/google/go-containerregistry/pkg/v1/daemon"
	"github.com/google/go-containerregistry/pkg/v1/remote"
)

// DockerRegistryOutputHandler writes Docker images to and loads them from a registry specified by configuration.
type DockerRegistryOutputHandler struct {
	targetCache *caching.TargetCache
	config      config.DockerConfig
}

// NewDockerRegistryOutputHandler creates a new DockerRegistryOutputHandler.
func NewDockerRegistryOutputHandler(
	targetCache *caching.TargetCache,
	config config.DockerConfig,
) *DockerRegistryOutputHandler {
	return &DockerRegistryOutputHandler{
		targetCache: targetCache,
		config:      config,
	}
}

func (d *DockerRegistryOutputHandler) Type() HandlerType {
	return DockerHandler
}

func (d *DockerRegistryOutputHandler) cacheImageName(target model.Target, output model.Output) string {
	workspaceDir := config.Global.WorkspaceRoot
	workspacePrefix := config.GetWorkspaceCachePrefix(workspaceDir)

	return fmt.Sprintf("%s/%s%s/%s", d.config.Registry,
		workspacePrefix, d.targetCache.CachePath(target), d.targetCache.CacheKey(output))
}

// Has checks if the Docker image exists in the remote registry.
func (d *DockerRegistryOutputHandler) Has(ctx context.Context, target model.Target, output model.Output) (bool, error) {
	logger := console.GetLogger(ctx)
	remoteImageName := d.cacheImageName(target, output)

	logger.Debugf("checking existence of Docker image %s in registry", remoteImageName)

	ref, err := name.ParseReference(remoteImageName)
	if err != nil {
		return false, fmt.Errorf("failed to parse image reference %q: %w", remoteImageName, err)
	}

	_, err = remote.Head(ref, remote.WithAuthFromKeychain(authn.DefaultKeychain))
	if err != nil {
		if isNotFound(err) {
			return false, nil
		}
		return false, fmt.Errorf("checking image existence: %w", err)
	}

	return true, nil
}

// Write pushes the Docker image from the local Docker daemon to the remote registry.
func (d *DockerRegistryOutputHandler) Write(ctx context.Context, target model.Target, output model.Output) error {
	logger := console.GetLogger(ctx)
	localImageName := output.Identifier
	remoteImageName := d.cacheImageName(target, output)

	logger.Debugf("pushing Docker image %s to registry as %s", localImageName, remoteImageName)

	// Get the image from the local Docker daemon.
	localRef, err := name.ParseReference(localImageName)
	if err != nil {
		return fmt.Errorf("failed to parse local image reference %q: %w", localImageName, err)
	}

	img, err := daemon.Image(localRef)
	if err != nil {
		return fmt.Errorf("failed to get image %q from local Docker daemon: %w", localImageName, err)
	}

	// Create the remote tag reference.
	remoteTag, err := name.NewTag(remoteImageName)
	if err != nil {
		return fmt.Errorf("failed to create remote tag %q: %w", remoteImageName, err)
	}

	// Push the image to the remote registry.
	if err := remote.Write(remoteTag, img, remote.WithAuthFromKeychain(authn.DefaultKeychain)); err != nil {
		return fmt.Errorf("failed to push image %q to registry: %w", remoteImageName, err)
	}

	logger.Debugf("successfully pushed Docker image %s to registry", remoteImageName)
	return nil
}

// Load pulls the Docker image from the remote registry and writes it into the local Docker daemon.
func (d *DockerRegistryOutputHandler) Load(ctx context.Context, target model.Target, output model.Output) error {
	logger := console.GetLogger(ctx)
	localImageName := output.Identifier
	remoteImageName := d.cacheImageName(target, output)

	logger.Debugf("pulling Docker image %s from registry", remoteImageName)

	// Create the remote tag reference
	remoteTag, err := name.NewTag(remoteImageName)
	if err != nil {
		return fmt.Errorf("failed to parse remote image tag %q: %w", remoteImageName, err)
	}

	// Get remote image digest
	remoteDesc, err := remote.Head(remoteTag, remote.WithAuthFromKeychain(authn.DefaultKeychain))
	if err != nil {
		return fmt.Errorf("failed to get remote image digest: %w", err)
	}

	// Check if image with same digest exists locally
	localRef, err := name.ParseReference(output.Identifier)
	if err != nil {
		return fmt.Errorf("failed to parse local image reference %q: %w", localImageName, err)
	}

	if localImg, err := daemon.Image(localRef); err == nil {
		localDigest, err := localImg.Digest()
		if err == nil && localDigest.String() == remoteDesc.Digest.String() {
			logger.Debugf("image %s with digest %s already exists locally, skipping pull", localImageName, localDigest)
			return nil
		}
	}

	// Pull the image from the remote registry
	img, err := remote.Image(remoteTag, remote.WithAuthFromKeychain(authn.DefaultKeychain))
	if err != nil {
		return fmt.Errorf("failed to pull image %q from registry: %w", remoteImageName, err)
	}

	// Write the image into the local Docker daemon
	writtenTag, err := daemon.Write(remoteTag, img)
	if err != nil {
		return fmt.Errorf("failed to write image %q to Docker daemon: %w", remoteImageName, err)
	}

	logger.Debugf("successfully loaded Docker image %s (written tag: %s) from registry", localImageName, writtenTag)
	logger.Infof("Loaded image %s from registry", localImageName)
	return nil
}

// isNotFound is a helper that determines whether an error indicates a 404 Not Found status.
func isNotFound(err error) bool {
	var terr *transport.Error
	if errors.As(err, &terr) {
		// Straight 404 at the HTTP layer:
		if terr.StatusCode == http.StatusNotFound {
			return true
		}
		// Some registries return a 404 with a JSON body like:
		//   {"errors":[{"code":"MANIFEST_UNKNOWN", …}]}
		for _, diag := range terr.Errors {
			if diag.Code == transport.ManifestUnknownErrorCode {
				return true
			}
		}
	}
	return false
}
