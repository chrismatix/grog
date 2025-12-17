package handlers

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"sync"

	"grog/internal/caching"
	"grog/internal/console"
	"grog/internal/hashing"
	"grog/internal/model"
	"grog/internal/proto/gen"
	"grog/internal/worker"

	"github.com/google/go-containerregistry/pkg/name"
	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/daemon"
	"github.com/google/go-containerregistry/pkg/v1/empty"
	"github.com/google/go-containerregistry/pkg/v1/mutate"
	"github.com/google/go-containerregistry/pkg/v1/tarball"
	"github.com/shirou/gopsutil/mem"
	"golang.org/x/sync/semaphore"
)

// DockerOutputHandler caches docker images either as tarball's or in a registry
type DockerOutputHandler struct {
	cas *caching.Cas

	// Shared across the handler instance: caps total concurrent in-flight layer bytes.
	maxInFlightBytes int64
	inFlightBytes    *semaphore.Weighted
}

// NewDockerOutputHandler creates a new DockerOutputHandler
func NewDockerOutputHandler(ctx context.Context, cas *caching.Cas) *DockerOutputHandler {
	logger := console.GetLogger(ctx)
	freeMemory, err := freeSystemMemoryBytes()
	if err != nil {
		logger.Warnf("failed to determine free system memory: %v", err)
		// Fall back to a very conservative default: 1024 MiB
		freeMemory = 1024 << 20
	} else {
		// By default only use half of the system memory for Docker layers.
		// TODO: We should turn this into a global budget
		freeMemory /= 2
	}

	// Ensure we never end up with a 0/negative budget ().
	if freeMemory < 64<<20 {
		freeMemory = 64 << 20 // 64 MiB minimum
	}
	logger.Debugf("using %d MiB of system memory for Docker layer caching", freeMemory>>20)

	return &DockerOutputHandler{
		cas:              cas,
		maxInFlightBytes: int64(freeMemory),
		inFlightBytes:    semaphore.NewWeighted(int64(freeMemory)),
	}
}

func freeSystemMemoryBytes() (uint64, error) {
	vm, err := mem.VirtualMemory()
	if err != nil {
		return 0, err
	}
	return vm.Available, nil
}

// Type returns the type of the handler
func (d *DockerOutputHandler) Type() HandlerType {
	return DockerHandler
}

// Hash hashes the local Docker image manifest which should be the source of truth for the image
func (d *DockerOutputHandler) Hash(ctx context.Context, _ model.Target, output model.Output) (string, error) {
	logger := console.GetLogger(ctx)
	imageName := output.Identifier

	logger.Debugf("hashing Docker image %s manifest", imageName)

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

	rawManifest, err := img.RawManifest()
	if err != nil {
		return "", fmt.Errorf("failed to read manifest for image %q: %w", imageName, err)
	}

	return hashing.HashBytes(rawManifest), nil
}

func (d *DockerOutputHandler) Write(
	ctx context.Context,
	target model.Target,
	output model.Output,
	tracker *worker.ProgressTracker,
) (*gen.Output, error) {
	logger := console.GetLogger(ctx)
	imageName := output.Identifier

	logger.Debugf("saving Docker image %s to content-addressable cache", imageName)

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

	// Collect manifest/config/layers
	manifest, err := img.Manifest()
	if err != nil {
		return nil, fmt.Errorf("failed to read manifest for image %q: %w", imageName, err)
	}
	manifestBytes, err := img.RawManifest()
	if err != nil {
		return nil, fmt.Errorf("failed to read raw manifest for image %q: %w", imageName, err)
	}
	manifestDigest := hashing.HashBytes(manifestBytes)

	configBytes, err := img.RawConfigFile()
	if err != nil {
		return nil, fmt.Errorf("failed to read config for image %q: %w", imageName, err)
	}
	configDigest := manifest.Config.Digest.String()
	configSize := manifest.Config.Size
	if configSize == 0 {
		configSize = int64(len(configBytes))
	}
	layers, err := img.Layers()
	if err != nil {
		return nil, fmt.Errorf("failed to read layers for image %q: %w", imageName, err)
	}
	if len(layers) != len(manifest.Layers) {
		return nil, fmt.Errorf("manifest for image %q describes %d layers but %d layers were loaded", imageName, len(manifest.Layers), len(layers))
	}
	imageId, err := img.ConfigName()
	if err != nil {
		return nil, fmt.Errorf("failed to compute image id for %q: %w", imageName, err)
	}

	totalBytes := int64(len(manifestBytes)) + configSize
	for _, layer := range manifest.Layers {
		totalBytes += layer.Size
	}

	progress := tracker
	if progress != nil {
		progress = progress.SubTracker(
			fmt.Sprintf("%s: writing docker image %s", target.Label, imageName),
			totalBytes,
		)
	}

	// Write config blob
	var configReader io.Reader = bytes.NewReader(configBytes)
	if progress != nil {
		configReader = progress.WrapReader(configReader)
	}
	logger.Debugf("writing Docker config %s for image %s to CAS", configDigest, imageName)
	if err := d.cas.Write(ctx, configDigest, configReader); err != nil {
		return nil, fmt.Errorf("failed to write config blob for image %s: %w", imageName, err)
	}

	// Write each layer individually
	var wg sync.WaitGroup
	errCh := make(chan error, len(layers))

	for idx, layer := range layers {
		wg.Add(1)
		go func(idx int, layer v1.Layer) {
			defer wg.Done()

			descriptor := manifest.Layers[idx]

			// Use descriptor.Size (compressed size) as the "cost" to bound global in-flight work.
			cost := descriptor.Size
			if cost <= 0 {
				cost = 1 << 20 // 1 MiB fallback to avoid zero-cost acquisitions
			}
			if cost > d.maxInFlightBytes {
				// Allow very large layers to run by themselves (serialize them against the cap).
				cost = d.maxInFlightBytes
			}

			if err := d.inFlightBytes.Acquire(ctx, cost); err != nil {
				errCh <- fmt.Errorf("failed to acquire in-flight budget for layer %d (%s): %w", idx, descriptor.Digest, err)
				return
			}
			defer d.inFlightBytes.Release(cost)

			layerReader, err := layer.Compressed()
			if err != nil {
				errCh <- fmt.Errorf("failed to open layer %d for image %s: %w", idx, imageName, err)
				return
			}
			defer layerReader.Close()

			reader := layerReader
			if progress != nil {
				reader = progress.WrapReadCloser(reader)
			}

			logger.Debugf("writing Docker layer %s for image %s to CAS", descriptor.Digest, imageName)
			if err := d.cas.Write(ctx, descriptor.Digest.String(), reader); err != nil {
				errCh <- fmt.Errorf("failed to write layer %s to cache: %w", descriptor.Digest, err)
				return
			}
		}(idx, layer)
	}

	wg.Wait()
	close(errCh)

	for err := range errCh {
		if err != nil {
			return nil, err
		}
	}
	// Write manifest blob
	var manifestReader io.Reader = bytes.NewReader(manifestBytes)
	if progress != nil {
		manifestReader = progress.WrapReader(manifestReader)
	}
	logger.Debugf("writing Docker manifest %s for image %s to CAS", manifestDigest, imageName)
	if err := d.cas.Write(ctx, manifestDigest, manifestReader); err != nil {
		return nil, fmt.Errorf("failed to write manifest for image %s: %w", imageName, err)
	}

	if progress != nil {
		progress.Complete()
	}

	logger.Debugf("successfully saved Docker image %s to cache", imageName)
	return &gen.Output{
		Kind: &gen.Output_DockerImage{
			DockerImage: &gen.DockerImageOutput{
				Mode:           gen.ImageMode_LAYERS,
				LocalTag:       imageName,
				ImageId:        imageId.String(),
				ManifestDigest: &gen.Digest{Hash: manifestDigest, SizeBytes: int64(len(manifestBytes))},
				ConfigDigest:   &gen.Digest{Hash: configDigest, SizeBytes: configSize},
			},
		},
	}, nil
}

// Load loads the Docker image from the cache and imports it into the Docker engine using go-containerregistry
func (d *DockerOutputHandler) Load(
	ctx context.Context,
	target model.Target,
	output *gen.Output,
	tracker *worker.ProgressTracker,
) error {
	dockerImage := output.GetDockerImage()
	if dockerImage.GetMode() != gen.ImageMode_LAYERS {
		return fmt.Errorf("cannot restore %s docker cache as layer cache is configured", dockerImage.GetMode())
	}
	return d.loadFromCasLayers(ctx, target, output.GetDockerImage(), tracker)
}

func (d *DockerOutputHandler) loadFromCasLayers(
	ctx context.Context,
	target model.Target,
	dockerImage *gen.DockerImageOutput,
	tracker *worker.ProgressTracker,
) error {
	logger := console.GetLogger(ctx)
	imageName := dockerImage.GetLocalTag()
	manifestDigest := dockerImage.GetManifestDigest()
	configDigest := dockerImage.GetConfigDigest()

	logger.Debugf("loading Docker image %s from CAS layers", imageName)

	ref, err := name.ParseReference(imageName)
	if err != nil {
		return fmt.Errorf("failed to parse image reference %q: %w", imageName, err)
	}

	if existingImg, err := daemon.Image(ref); err == nil {
		existingImageId, err := existingImg.ConfigName()
		if err == nil && dockerImage.GetImageId() != "" && existingImageId.String() == dockerImage.GetImageId() {
			logger.Debugf("image %s already exists locally so skipping load", imageName)
			return nil
		}
	}

	logger.Debugf("loading Docker manifest %s for image %s from CAS", manifestDigest.GetHash(), imageName)
	manifestBytes, err := d.cas.LoadBytes(ctx, manifestDigest.GetHash())
	if err != nil {
		return fmt.Errorf("failed to read manifest for image %s from cache: %w", imageName, err)
	}

	manifest, err := v1.ParseManifest(bytes.NewReader(manifestBytes))
	if err != nil {
		return fmt.Errorf("failed to parse manifest for image %s: %w", imageName, err)
	}

	manifestSize := manifestDigest.GetSizeBytes()
	if manifestSize == 0 {
		manifestSize = int64(len(manifestBytes))
	}

	logger.Debugf("loading Docker config %s for image %s from CAS", configDigest.GetHash(), imageName)
	configReader, err := d.cas.Load(ctx, configDigest.GetHash())
	if err != nil {
		return fmt.Errorf("failed to read config for image %s from cache: %w", imageName, err)
	}
	defer configReader.Close()

	progress := tracker
	totalBytes := manifestSize + configDigest.GetSizeBytes()
	for _, layer := range manifest.Layers {
		totalBytes += layer.Size
	}

	if progress != nil {
		progress = progress.SubTracker(
			fmt.Sprintf("%s: loading docker image %s", target.Label, imageName),
			totalBytes,
		)
		configReader = struct {
			io.Reader
			io.Closer
		}{
			Reader: progress.WrapReader(configReader),
			Closer: configReader,
		}
	}

	configBytes, err := io.ReadAll(configReader)
	if err != nil {
		return fmt.Errorf("failed to read config bytes for image %s: %w", imageName, err)
	}

	configFile, err := v1.ParseConfigFile(bytes.NewReader(configBytes))
	if err != nil {
		return fmt.Errorf("failed to parse config for image %s: %w", imageName, err)
	}

	layers := make([]v1.Layer, 0, len(manifest.Layers))
	for idx, descriptor := range manifest.Layers {
		desc := descriptor // capture
		opener := func() (io.ReadCloser, error) {
			logger.Debugf("loading Docker layer %s for image %s from CAS", desc.Digest.String(), imageName)
			reader, err := d.cas.Load(ctx, desc.Digest.String())
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
		}
		layer, err := tarball.LayerFromOpener(opener, tarball.WithMediaType(desc.MediaType))
		if err != nil {
			return fmt.Errorf("failed to reconstruct layer %s for image %s: %w", desc.Digest, imageName, err)
		}
		layers = append(layers, layer)
		_ = idx
	}

	imgWithConfig, err := mutate.ConfigFile(empty.Image, configFile)
	if err != nil {
		return fmt.Errorf("failed to set config for image %s: %w", imageName, err)
	}

	imgWithConfig = mutate.ConfigMediaType(imgWithConfig, manifest.Config.MediaType)

	imgWithLayers, err := mutate.AppendLayers(imgWithConfig, layers...)
	if err != nil {
		return fmt.Errorf("failed to append layers for image %s: %w", imageName, err)
	}

	imgWithLayers = mutate.MediaType(imgWithLayers, manifest.MediaType)

	tag, err := name.NewTag(imageName)
	if err != nil {
		return fmt.Errorf("failed to parse image tag %q: %w", imageName, err)
	}

	writtenTag, err := daemon.Write(tag, imgWithLayers)
	if err != nil {
		return fmt.Errorf("failed to write image %q to Docker daemon: %w", imageName, err)
	}

	logger.Debugf("successfully loaded Docker image %s (written tag: %s)", imageName, writtenTag)
	logger.Infof("Loaded image %s from cache backend", imageName)

	if progress != nil {
		progress.Complete()
	}

	return nil
}

func (d *DockerOutputHandler) getCachedTarballSize(ctx context.Context, digest string, tag name.Tag) (int64, error) {
	console.GetLogger(ctx).Debugf("loading cached Docker tarball %s", digest)
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
