package handlers

import (
	"context"
	"fmt"
	"grog/internal/caching"
	"grog/internal/console"
	"grog/internal/model"
	"grog/internal/proto/gen"
	"io"

	"github.com/cespare/xxhash/v2"
	"github.com/google/go-containerregistry/pkg/name"
	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/daemon"
	"github.com/google/go-containerregistry/pkg/v1/tarball"
)

// DockerOutputHandler caches docker images either as tarball's or in a registry
type DockerOutputHandler struct {
	cas *caching.Cas
}

// NewDockerOutputHandler creates a new DockerOutputHandler
func NewDockerOutputHandler(cas *caching.Cas) *DockerOutputHandler {
	return &DockerOutputHandler{
		cas: cas,
	}
}

// Type returns the type of the handler
func (d *DockerOutputHandler) Type() HandlerType {
	return DockerHandler
}

// Has checks if the Docker image exists in the cache
func (d *DockerOutputHandler) Has(ctx context.Context, output *gen.Output) (bool, error) {
	// We check for the existence of the tarball in the cache
	return d.cas.Exists(ctx, output.GetDockerImage().GetDigest())
}

// Write saves the Docker image as a tarball and stores it in the cache using go-containerregistry
func (d *DockerOutputHandler) Write(ctx context.Context, target model.Target, output model.Output) (*gen.Output, error) {
	logger := console.GetLogger(ctx)
	imageName := output.Identifier

	logger.Debugf("saving Docker image %s to tarball", imageName)

	// Parse the image reference
	ref, err := name.ParseReference(imageName)
	if err != nil {
		return nil, fmt.Errorf("failed to parse image reference %q: %w", imageName, err)
	}

	// Get the image from the Docker daemon
	img, err := daemon.Image(ref)
	if err != nil {
		return nil, fmt.Errorf("failed to get image %q from Docker daemon: %w", imageName, err)
	}

	manifestDigest, err := img.Digest()
	if err != nil {
		return nil, fmt.Errorf("failed to get image manifest digest for image %q: %w", imageName, err)
	}

	hashReader := getTarballReader(ref, img)
	hasher := xxhash.New()
	_, err = io.Copy(hasher, hashReader)
	if err != nil {
		hashReader.Close()
		return nil, fmt.Errorf("failed to hash Docker image tarball for image %q: %w", imageName, err)
	}

	digest := fmt.Sprintf("%x", hasher.Sum64())
	// Get a completely new reader for actually writing the `
	writeReader := getTarballReader(ref, img)

	// Stream the tarball from the pipe reader to the cache
	logger.Debugf("streaming Docker image tarball to cache")
	err = d.cas.Write(ctx, digest, writeReader)
	if err != nil {
		// Ensure the pipe reader is closed even if WriteFileStream fails
		writeReader.Close()
		return nil, fmt.Errorf("failed to write tarball stream to cache for image %s: %w", imageName, err)
	}

	logger.Debugf("successfully saved Docker image %s to cache", imageName)
	return &gen.Output{
		Kind: &gen.Output_DockerImage{
			DockerImage: &gen.DockerImageOutput{
				Digest:         digest,
				LocalTag:       imageName,
				ManifestDigest: manifestDigest.String(),
				Mode:           gen.ImageMode_TAR,
			},
		},
	}, nil
}

func getTarballReader(ref name.Reference, img v1.Image) (pipeRead io.ReadCloser) {
	// Create a pipe to stream the tarball directly to the cache
	pipeRead, pipeWrite := io.Pipe()

	// Write the image to the tarball stream in a separate goroutine
	go func() {
		defer pipeWrite.Close() // Close the writer when tarball.Write completes or errors
		if err := tarball.Write(ref, img, pipeWrite); err != nil {
			// Propagate the error by closing the pipe writer with the error
			pipeWrite.CloseWithError(fmt.Errorf("failed to write image %q to tarball stream: %w", ref.Name(), err))
		}
	}()

	return pipeRead
}

// Load loads the Docker image tarball from the cache and imports it into the Docker engine using go-containerregistry
func (d *DockerOutputHandler) Load(ctx context.Context, _ model.Target, output *gen.Output) error {
	logger := console.GetLogger(ctx)
	// The original image name/tag used when saving
	imageName := output.GetDockerImage().GetLocalTag()

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
		return d.cas.Load(ctx, output.GetDockerImage().GetDigest())
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
