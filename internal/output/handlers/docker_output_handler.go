package handlers

import (
	"context"
	"fmt"
	"io"

	"grog/internal/caching"
	"grog/internal/console"
	"grog/internal/hashing"
	"grog/internal/model"
	"grog/internal/proto/gen"
	"grog/internal/worker"

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

func (d *DockerOutputHandler) Hash(ctx context.Context, _ model.Target, output model.Output) (string, error) {
	logger := console.GetLogger(ctx)
	imageName := output.Identifier

	logger.Debugf("saving Docker image %s to tarball", imageName)

	// Parse the image reference
	ref, err := name.ParseReference(imageName)
	if err != nil {
		return "", fmt.Errorf("failed to parse image reference %q: %w", imageName, err)
	}

	// Get the image from the Docker daemon
	img, err := daemon.Image(ref)
	if err != nil {
		return "", fmt.Errorf("failed to get image %q from Docker daemon: %w", imageName, err)
	}

	digest, _, err := hashLocalTarball(ref, img)
	if err != nil {
		return "", fmt.Errorf("failed to hash Docker image tarball for image %q: %w", imageName, err)
	}

	return digest, nil
}

// Write saves the Docker image as a tarball and stores it in the cache using go-containerregistry
func (d *DockerOutputHandler) Write(
	ctx context.Context,
	target model.Target,
	output model.Output,
	tracker *worker.ProgressTracker,
) (*gen.Output, error) {
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

	localDigest, tarballSize, err := hashLocalTarball(ref, img)
	if err != nil {
		return nil, err
	}
	// Get a completely new reader for actually writing the image
	writeReader := getTarballReader(ref, img)
	defer writeReader.Close()

	progress := tracker
	if progress != nil {
		progress = progress.SubTracker(
			fmt.Sprintf("%s: writing docker image %s", target.Label, imageName),
			tarballSize,
		)
	}

	reader := io.Reader(writeReader)
	if progress != nil {
		reader = progress.WrapReader(writeReader)
	}

	// Stream the tarball from the pipe reader to the cache
	logger.Debugf("streaming Docker image tarball to cache")
	err = d.cas.Write(ctx, localDigest, reader)
	if err != nil {
		// Ensure the pipe reader is closed even if WriteFileStream fails
		writeReader.Close()
		return nil, fmt.Errorf("failed to write tarball stream to cache for image %s: %w", imageName, err)
	}

	if progress != nil {
		progress.Complete()
	}

	logger.Debugf("successfully saved Docker image %s to cache", imageName)
	return &gen.Output{
		Kind: &gen.Output_DockerImage{
			DockerImage: &gen.DockerImageOutput{
				TarDigest: localDigest,
				LocalTag:  imageName,
				Mode:      gen.ImageMode_TAR,
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

func hashLocalTarball(ref name.Reference, img v1.Image) (string, int64, error) {
	hashReader := getTarballReader(ref, img)
	defer hashReader.Close()
	hasher := hashing.GetHasher()
	size, err := io.Copy(hasher, hashReader)
	if err != nil {
		return "", 0, fmt.Errorf("failed to hash Docker image tarball for image %q: %w", ref.Name(), err)
	}
	return hasher.SumString(), size, nil
}

// Load loads the Docker image tarball from the cache and imports it into the Docker engine using go-containerregistry
func (d *DockerOutputHandler) Load(
	ctx context.Context,
	target model.Target,
	output *gen.Output,
	tracker *worker.ProgressTracker,
) error {
	logger := console.GetLogger(ctx)
	// The original image name/tag used when saving
	imageName := output.GetDockerImage().GetLocalTag()
	digest := output.GetDockerImage().GetTarDigest()

	logger.Debugf("loading Docker image %s from cache using go-containerregistry", imageName)

	// Parse the original reference to tag the image correctly after loading
	ref, err := name.ParseReference(imageName)
	if err != nil {
		return fmt.Errorf("failed to parse image reference %q: %w", imageName, err)
	}

	// Get the *local* image from the Docker daemon
	existingImg, err := daemon.Image(ref)
	if err == nil {
		localDigest, _, err := hashLocalTarball(ref, existingImg)
		if err == nil && digest == localDigest {
			// The image already exists locally so no need to load it
			logger.Debugf("image %s already exists locally so skipping load", imageName)
			return nil
		}
	}

	tag, err := name.NewTag(imageName)
	if err != nil {
		return fmt.Errorf("failed to parse image tag %q: %w", imageName, err)
	}

	progress := tracker
	if progress != nil {
		tarballSize, err := d.getCachedTarballSize(ctx, digest, tag)
		if err != nil {
			return err
		}

		if tarballSize > 0 {
			progress = progress.SubTracker(
				fmt.Sprintf("%s: loading docker image %s", target.Label, imageName),
				tarballSize,
			)
		}
	}

	img, err := tarball.Image(func() (io.ReadCloser, error) {
		reader, err := d.cas.Load(ctx, digest)
		if err != nil {
			return nil, err
		}

		if progress != nil {
			return struct {
				io.Reader
				io.Closer
			}{
				Reader: progress.WrapReader(reader),
				Closer: reader,
			}, nil
		}

		return reader, nil
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

	if progress != nil {
		progress.Complete()
	}

	return nil
}

func (d *DockerOutputHandler) getCachedTarballSize(ctx context.Context, digest string, tag name.Tag) (int64, error) {
	img, err := tarball.Image(func() (io.ReadCloser, error) {
		return d.cas.Load(ctx, digest)
	}, &tag)
	if err != nil {
		return 0, fmt.Errorf("failed to read cached tarball for digest %s: %w", digest, err)
	}

	tarballSize, err := img.Size()
	if err != nil {
		return 0, fmt.Errorf("failed to get size for cached tarball %s: %w", digest, err)
	}

	return tarballSize, nil
}
