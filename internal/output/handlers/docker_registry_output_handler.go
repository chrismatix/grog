package handlers

import (
	"context"
	"fmt"
	"github.com/google/go-containerregistry/pkg/authn"
	"grog/internal/caching"
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
	return d.targetCache.HasOutputMetaFile(ctx, target, output, "digest")
}

// Write pushes the Docker image from the local Docker daemon to the remote registry.
func (d *DockerRegistryOutputHandler) Write(ctx context.Context, target model.Target, output model.Output) error {
	logger := console.GetLogger(ctx)
	localImageName := output.Identifier
	remoteCacheImageName := d.cacheImageName(target, output)

	logger.Debugf("pushing Docker image %s to cache registry as %s", localImageName, remoteCacheImageName)

	// Get the image from the local Docker daemon.
	localRef, err := name.ParseReference(localImageName)
	if err != nil {
		return fmt.Errorf("failed to parse local image reference %q: %w", localImageName, err)
	}

	img, err := daemon.Image(localRef, daemon.WithContext(ctx))
	if err != nil {
		return fmt.Errorf("failed to get image %q from local Docker daemon: %w", localImageName, err)
	}

	// Create the remote tag reference.
	remoteTag, err := name.NewTag(remoteCacheImageName)
	if err != nil {
		return fmt.Errorf("failed to create remote tag %q: %w", remoteCacheImageName, err)
	}

	// Push the image to the remote cache registry.
	if err := remote.Write(remoteTag,
		img, remote.WithAuthFromKeychain(authn.DefaultKeychain), remote.WithContext(ctx)); err != nil {
		return fmt.Errorf("failed to push image %q to registry: %w", remoteCacheImageName, err)
	}

	digest, err := img.ConfigName()
	if err != nil {
		return fmt.Errorf("failed to get image digest: %w", err)
	}

	err = d.targetCache.WriteOutputMetaFile(ctx, target, output, "digest", digest.String())
	if err != nil {
		return fmt.Errorf("failed to write digest to cache: %w", err)
	}

	logger.Debugf("successfully pushed Docker image %s to registry", remoteCacheImageName)
	return nil
}

// Load pulls the Docker image from the remote registry and writes it into the local Docker daemon.
func (d *DockerRegistryOutputHandler) Load(ctx context.Context, target model.Target, output model.Output) error {
	imageName := output.Identifier
	// Get expected image digest
	expectedDigest, err := d.targetCache.LoadOutputMetaFile(ctx, target, output, "digest")
	if err != nil {
		return fmt.Errorf("failed to load digest file %q: %w", imageName, err)
	}

	logger := console.GetLogger(ctx)
	localImageName := output.Identifier

	localTag, err := name.NewTag(localImageName)
	if err != nil {
		return fmt.Errorf("failed to parse local image tag %q: %w", localImageName, err)
	}

	// Ensure that we are only looking locally
	localTag.Repository = name.Repository{}
	if img, err := daemon.Image(localTag, daemon.WithContext(ctx)); err == nil {
		digest, err := img.ConfigName()
		if err == nil && digest.String() == expectedDigest {
			logger.Debugf("image %s found locally with matching digest %s, skipping registry lookup", localTag, expectedDigest)
			return nil
		}
		logger.Debugf("image %s found locally but digest mismatch (got %s, want %s)", localTag, digest.String(), expectedDigest)
	} else {
		logger.Debugf("image %s not found locally: %v", localTag, err)
	}

	remoteImageName := d.cacheImageName(target, output)
	logger.Debugf("pulling Docker image %s from registry", remoteImageName)

	// Create the local tag reference
	remoteTag, err := name.NewTag(remoteImageName)
	if err != nil {
		return fmt.Errorf("failed to parse remote image tag %q: %w", remoteImageName, err)
	}

	// Pull the image from the remote registry
	img, err := remote.Image(remoteTag, remote.WithAuthFromKeychain(authn.DefaultKeychain))
	if err != nil {
		return fmt.Errorf("failed to pull image %q from registry: %w", remoteImageName, err)
	}

	// Write the image into the local Docker daemon
	_, err = daemon.Write(localTag, img, daemon.WithContext(ctx))
	if err != nil {
		return fmt.Errorf("failed to write image %q to Docker daemon: %w", remoteImageName, err)
	}

	logger.Debugf("successfully loaded Docker image %s from registry tag %s", localImageName, remoteTag)
	logger.Infof("Loaded image %s from registry", localImageName)
	return nil
}
