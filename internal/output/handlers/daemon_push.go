package handlers

import (
	"context"
	"fmt"

	"github.com/docker/docker/api/types/image"
	"github.com/docker/docker/client"

	"grog/internal/worker"
)

// pushImageFromDaemon is the fallback for images that never reached the cache.
// Unlike the daemon-free copy path it takes credentials and plain-HTTP
// decisions from the daemon's own configuration.
func pushImageFromDaemon(
	ctx context.Context,
	dockerClient *client.Client,
	localTag string,
	destination string,
	tracker *worker.ProgressTracker,
) error {
	if err := dockerClient.ImageTag(ctx, localTag, destination); err != nil {
		return fmt.Errorf("failed to tag image %q as %q: %w", localTag, destination, err)
	}

	auth, err := makeRegistryAuth(destination)
	if err != nil {
		return err
	}

	pushReader, err := dockerClient.ImagePush(ctx, destination, image.PushOptions{RegistryAuth: auth})
	if err != nil {
		return fmt.Errorf("failed to push image %q to %q: %w", localTag, destination, err)
	}
	defer pushReader.Close()

	pushedDigest, err := consumeDockerProgress(pushReader, tracker, fmt.Sprintf("pushing %s", destination))
	if err != nil {
		return fmt.Errorf("error reading push response: %w", err)
	}
	// The daemon reports a digest only once the registry has acknowledged the
	// manifest, so its absence means nothing is known to have landed.
	if pushedDigest == "" {
		return fmt.Errorf("push to %q finished without a registry digest, so the image is not known to have landed", destination)
	}
	return nil
}
