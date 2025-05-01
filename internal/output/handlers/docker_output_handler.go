package handlers

import (
	"context"
	"fmt"
	"grog/internal/caching"
	"grog/internal/console"
	"grog/internal/model"
	"io"

	"github.com/google/go-containerregistry/pkg/name"
	"github.com/google/go-containerregistry/pkg/v1/daemon"
	"github.com/google/go-containerregistry/pkg/v1/tarball"
)

// DockerOutputHandler caches docker images either as tarball's or in a registry
type DockerOutputHandler struct {
	targetCache *caching.TargetCache
}

// NewDockerOutputHandler creates a new DockerOutputHandler
func NewDockerOutputHandler(targetCache *caching.TargetCache) *DockerOutputHandler {
	return &DockerOutputHandler{
		targetCache: targetCache,
	}
}

// Type returns the type of the handler
func (d *DockerOutputHandler) Type() HandlerType {
	return DockerHandler
}

// Has checks if the Docker image exists in the cache
func (d *DockerOutputHandler) Has(ctx context.Context, target model.Target, output model.Output) (bool, error) {
	// We check for the existence of the tarball in the cache
	return d.targetCache.FileExists(ctx, target, output)
}

// Write saves the Docker image as a tarball and stores it in the cache using go-containerregistry
func (d *DockerOutputHandler) Write(ctx context.Context, target model.Target, output model.Output) error {
	logger := console.GetLogger(ctx)
	imageName := output.Identifier

	logger.Debugf("saving Docker image %s to tarball using go-containerregistry", imageName)

	// Parse the image reference
	ref, err := name.ParseReference(imageName)
	if err != nil {
		return fmt.Errorf("failed to parse image reference %q: %w", imageName, err)
	}

	// Get the image from the Docker daemon
	img, err := daemon.Image(ref)
	if err != nil {
		return fmt.Errorf("failed to get image %q from Docker daemon: %w", imageName, err)
	}

	// Create a pipe to stream the tarball directly to the cache
	pr, pw := io.Pipe()

	// Write the image to the tarball stream in a separate goroutine
	go func() {
		defer pw.Close() // Close the writer when tarball.Write completes or errors
		if err := tarball.Write(ref, img, pw); err != nil {
			// Propagate the error by closing the pipe writer with the error
			pw.CloseWithError(fmt.Errorf("failed to write image %q to tarball stream: %w", imageName, err))
		}
	}()

	// Stream the tarball from the pipe reader to the cache
	logger.Debugf("streaming Docker image tarball to cache")
	err = d.targetCache.WriteFileStream(ctx, target, output, pr)
	if err != nil {
		// Ensure the pipe reader is closed even if WriteFileStream fails
		pr.Close()
		// If the error came from the tarball writing goroutine, it will be returned here
		// If WriteFileStream itself failed, that error is returned
		return fmt.Errorf("failed to write tarball stream to cache for image %q: %w", imageName, err)
	}

	logger.Debugf("successfully saved Docker image %s to cache", imageName)
	return nil
}

// Load loads the Docker image tarball from the cache and imports it into the Docker engine using go-containerregistry
func (d *DockerOutputHandler) Load(ctx context.Context, target model.Target, output model.Output) error {
	logger := console.GetLogger(ctx)
	imageName := output.Identifier // The original image name/tag used when saving

	logger.Debugf("loading Docker image %s from cache using go-containerregistry", imageName)

	// Parse the original reference to tag the image correctly after loading
	_, err := name.ParseReference(imageName)
	if err != nil {
		return fmt.Errorf("failed to parse image reference %q: %w", imageName, err)
	}

	tag, err := name.NewTag(imageName)
	if err != nil {
		return fmt.Errorf("failed to parse image tag %q: %w", imageName, err)
	}

	img, err := tarball.Image(func() (io.ReadCloser, error) {
		return d.targetCache.LoadFileStream(ctx, target, output)
	}, &tag)
	if err != nil {
		return fmt.Errorf("failed to read image from tarball stream for %q: %w", imageName, err)
	}

	writtenTag, err := daemon.Write(tag, img)
	if err != nil {
		return fmt.Errorf("failed to write image %q to Docker daemon: %w", imageName, err)
	}
	logger.Debugf("successfully loaded Docker image %s (written tag: %s)", imageName, writtenTag)
	logger.Infof("Loaded image %s (tar)", imageName)

	return nil
}
