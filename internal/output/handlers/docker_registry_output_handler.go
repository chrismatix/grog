package handlers

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	dockerconfig "github.com/docker/cli/cli/config"
	"github.com/docker/docker/api/types/image"

	"grog/internal/caching"
	"grog/internal/config"
	"grog/internal/console"
	"grog/internal/model"

	"github.com/docker/docker/client"
	"github.com/google/go-containerregistry/pkg/authn"

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
func (d *DockerRegistryOutputHandler) Write(ctx context.Context, target model.Target, output model.Output) (string, error) {
	logger := console.GetLogger(ctx)
	localImageName := output.Identifier
	remoteCacheImageName := d.cacheImageName(target, output)

	logger.Debugf("pushing Docker image %s to cache registry as %s", localImageName, remoteCacheImageName)

	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		return "", fmt.Errorf("failed to create docker client: %w", err)
	}
	defer cli.Close()
	if err := cli.ImageTag(ctx, localImageName, remoteCacheImageName); err != nil {
		return "", fmt.Errorf("failed to tag image %q as %q: %w", localImageName, remoteCacheImageName, err)
	}
	// Clean up the image tag so that it does not pollute the user's docker machine
	defer cli.ImageRemove(ctx, remoteCacheImageName, image.RemoveOptions{})

	// Build the RegistryAuth header from ~/.docker/config.json / helpers
	auth, err := makeRegistryAuth(remoteCacheImageName)
	if err != nil {
		return "", err
	}

	// Push via Docker daemon using that auth
	reader, err := cli.ImagePush(ctx, remoteCacheImageName, image.PushOptions{
		RegistryAuth: auth,
	})
	if err != nil {
		return "", fmt.Errorf("failed to push image %q to registry: %w", remoteCacheImageName, err)
	}
	defer reader.Close()

	if _, err := io.Copy(io.Discard, reader); err != nil {
		return "", fmt.Errorf("error reading push response: %w", err)
	}

	inspect, _, err := cli.ImageInspectWithRaw(ctx, remoteCacheImageName)
	if err != nil {
		return "", fmt.Errorf("failed to inspect pushed image %q: %w", remoteCacheImageName, err)
	}

	err = d.targetCache.WriteOutputDigest(ctx, target, output, inspect.ID)
	if err != nil {
		return "", fmt.Errorf("failed to write digest to cache: %w", err)
	}

	logger.Debugf("successfully pushed Docker image %s to registry", remoteCacheImageName)
	return inspect.ID, nil
}

func makeRegistryAuth(ref string) (string, error) {
	// Extract registry hostname (e.g. "gcr.io" or "myregistry.example.com")
	parts := strings.SplitN(ref, "/", 2)
	registry := parts[0]

	// Load CLI config (respects DOCKER_CONFIG / XDG_CONFIG_HOME / ~/.docker)
	cfg, err := dockerconfig.Load("")
	if err != nil {
		return "", fmt.Errorf("loading docker config: %w", err)
	}

	// Get the AuthConfig for this registry
	authConfig, err := cfg.GetAuthConfig(registry)
	if err != nil {
		return "", fmt.Errorf("getting auth config for registry %q: %w", registry, err)
	}

	// JSON-encode and base64-encode it for the daemon API
	raw, err := json.Marshal(authConfig)
	if err != nil {
		return "", fmt.Errorf("marshaling auth config: %w", err)
	}
	return base64.URLEncoding.EncodeToString(raw), nil
}

// Load pulls the Docker image from the remote registry and writes it into the local Docker daemon.
func (d *DockerRegistryOutputHandler) Load(ctx context.Context, target model.Target, output model.Output) (string, error) {
	imageName := output.Identifier
	// Get expected image digest
	expectedDigest, err := d.targetCache.LoadOutputDigest(ctx, target, output)
	if err != nil {
		return "", fmt.Errorf("failed to load digest file %q: %w", imageName, err)
	}

	logger := console.GetLogger(ctx)
	localImageName := output.Identifier

	localTag, err := name.NewTag(localImageName)
	if err != nil {
		return "", fmt.Errorf("failed to parse local image tag %q: %w", localImageName, err)
	}

	// Ensure that we are only looking locally
	localTag.Repository = name.Repository{}
	if img, err := daemon.Image(localTag, daemon.WithContext(ctx)); err == nil {
		digest, err := img.ConfigName()
		if err == nil && digest.String() == expectedDigest {
			logger.Debugf("image %s found locally with matching digest %s, skipping registry lookup", localTag, expectedDigest)
			return expectedDigest, nil
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
		return "", fmt.Errorf("failed to parse remote image tag %q: %w", remoteImageName, err)
	}

	// Pull the image from the remote registry
	img, err := remote.Image(remoteTag, remote.WithAuthFromKeychain(authn.DefaultKeychain))
	if err != nil {
		return "", fmt.Errorf("failed to pull image %q from registry: %w", remoteImageName, err)
	}

	// Write the image into the local Docker daemon
	_, err = daemon.Write(localTag, img, daemon.WithContext(ctx))
	if err != nil {
		return "", fmt.Errorf("failed to write image %q to Docker daemon: %w", remoteImageName, err)
	}

	logger.Debugf("successfully loaded Docker image %s from registry tag %s", localImageName, remoteTag)
	logger.Infof("Loaded image %s from registry", localImageName)
	return expectedDigest, nil
}
