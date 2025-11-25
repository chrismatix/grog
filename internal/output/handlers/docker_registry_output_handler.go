package handlers

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"grog/internal/proto/gen"
	"io"
	"strings"

	dockerconfig "github.com/docker/cli/cli/config"
	"github.com/docker/docker/api/types/image"

	"grog/internal/caching"
	"grog/internal/config"
	"grog/internal/console"
	"grog/internal/model"

	"github.com/docker/docker/client"
)

// DockerRegistryOutputHandler writes Docker images to and loads them from a registry specified by configuration.
type DockerRegistryOutputHandler struct {
	cas          *caching.Cas
	config       config.DockerConfig
	dockerClient *client.Client
}

// NewDockerRegistryOutputHandler creates a new DockerRegistryOutputHandler.
func NewDockerRegistryOutputHandler(
	cas *caching.Cas,
	config config.DockerConfig,
) *DockerRegistryOutputHandler {
	return &DockerRegistryOutputHandler{
		cas:    cas,
		config: config,
	}
}

func (d *DockerRegistryOutputHandler) Type() HandlerType {
	return DockerHandler
}

func (d *DockerRegistryOutputHandler) Hash(ctx context.Context, target model.Target, output model.Output) (string, error) {
	cli, err := d.lazyClient()
	if err != nil {
		return "", err
	}

	localImageName := output.Identifier
	inspect, err := cli.ImageInspect(ctx, localImageName)
	if err != nil {
		return "", fmt.Errorf("%s: image output %s was not created: %w", target.Label.String(), localImageName, err)
	}

	return inspect.ID, nil
}

// lazyCient creates a new Docker client on demand
func (d *DockerRegistryOutputHandler) lazyClient() (*client.Client, error) {
	if d.dockerClient != nil {
		return d.dockerClient, nil
	}
	dockerClient, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		return nil, fmt.Errorf("failed to create docker client: %w", err)
	}
	d.dockerClient = dockerClient
	return d.dockerClient, nil
}

func (d *DockerRegistryOutputHandler) cacheImageName(digest string) string {
	workspaceDir := config.Global.WorkspaceRoot
	workspacePrefix := config.GetWorkspaceCachePrefix(workspaceDir)

	return fmt.Sprintf("%s/%s-%s", d.config.Registry,
		workspacePrefix, digest)
}

// Write pushes the Docker image from the local Docker daemon to the remote registry.
func (d *DockerRegistryOutputHandler) Write(ctx context.Context, target model.Target, output model.Output) (*gen.Output, error) {
	logger := console.GetLogger(ctx)
	localImageName := output.Identifier

	cli, err := d.lazyClient()
	if err != nil {
		return nil, err
	}

	inspect, err := cli.ImageInspect(ctx, localImageName)
	if err != nil {
		return nil, fmt.Errorf("%s: image output %s was not created: %w", target.Label.String(), localImageName, err)
	}

	remoteCacheImageName := d.cacheImageName(inspect.ID)

	logger.Debugf("pushing Docker image %s to cache registry as %s", localImageName, remoteCacheImageName)

	if err := cli.ImageTag(ctx, localImageName, remoteCacheImageName); err != nil {
		return nil, fmt.Errorf("failed to tag image %q as %q: %w", localImageName, remoteCacheImageName, err)
	}
	// Clean up the image tag so that it does not pollute the user's docker machine
	defer cli.ImageRemove(ctx, remoteCacheImageName, image.RemoveOptions{})

	// Build the RegistryAuth header from ~/.docker/config.json / helpers
	auth, err := makeRegistryAuth(remoteCacheImageName)
	if err != nil {
		return nil, err
	}

	// Push via Docker daemon using that auth
	reader, err := cli.ImagePush(ctx, remoteCacheImageName, image.PushOptions{
		RegistryAuth: auth,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to push image %q to registry: %w", remoteCacheImageName, err)
	}
	defer reader.Close()

	if _, err := io.Copy(io.Discard, reader); err != nil {
		return nil, fmt.Errorf("error reading push response: %w", err)
	}

	logger.Debugf("successfully pushed Docker image %s to registry", remoteCacheImageName)
	return &gen.Output{
		Kind: &gen.Output_DockerImage{
			DockerImage: &gen.DockerImageOutput{
				LocalTag:  localImageName,
				RemoteTag: remoteCacheImageName,
				ImageId:   inspect.ID,
				Mode:      gen.ImageMode_REGISTRY,
			},
		},
	}, nil
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
func (d *DockerRegistryOutputHandler) Load(ctx context.Context, _ model.Target, output *gen.Output) error {
	localImageName := output.GetDockerImage().GetLocalTag()
	imageId := output.GetDockerImage().GetImageId()

	logger := console.GetLogger(ctx)

	cli, err := d.lazyClient()
	if err != nil {
		return err
	}

	// check if the image exists in the local Docker daemon
	if _, err = cli.ImageInspect(ctx, imageId); err == nil {
		logger.Debugf("image %s already exists in local Docker daemon, skipping pull", localImageName)
		return nil
	}

	remoteImageName := output.GetDockerImage().GetRemoteTag()
	logger.Debugf("pulling Docker image %s from registry", remoteImageName)

	// Build the RegistryAuth header from ~/.docker/config.json / helpers
	auth, err := makeRegistryAuth(remoteImageName)
	if err != nil {
		return err
	}

	pull, err := cli.ImagePull(ctx, remoteImageName, image.PullOptions{
		RegistryAuth: auth,
	})
	if err != nil {
		return fmt.Errorf("failed to pull image %q from registry: %w", remoteImageName, err)
	}
	defer pull.Close()

	// Clean up the image tag so that it does not pollute the user's docker machine
	defer func() {
		if _, err := cli.ImageRemove(ctx, remoteImageName, image.RemoveOptions{}); err != nil {
			logger.Warnf("failed to remove image %q from local Docker daemon: %v", remoteImageName, err)
		}
	}()

	if err := cli.ImageTag(ctx, remoteImageName, localImageName); err != nil {
		return fmt.Errorf("failed to tag cache image %q as %q: %w", remoteImageName, localImageName, err)
	}

	logger.Debugf("successfully loaded Docker image %s from registry tag %s", localImageName, remoteImageName)
	logger.Infof("Loaded image %s from registry", localImageName)
	return nil
}
