package handlers

import (
	"bytes"
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
	"github.com/google/go-containerregistry/pkg/v1/empty"
	"github.com/google/go-containerregistry/pkg/v1/mutate"
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

type imageArtifacts struct {
	manifest       *v1.Manifest
	manifestBytes  []byte
	manifestDigest string
	manifestSize   int64
	configBytes    []byte
	configDigest   string
	configSize     int64
	layers         []v1.Layer
}

func collectImageArtifacts(imageName string, img v1.Image) (*imageArtifacts, error) {
	manifest, err := img.Manifest()
	if err != nil {
		return nil, fmt.Errorf("failed to read manifest for image %q: %w", imageName, err)
	}

	manifestBytes, err := img.RawManifest()
	if err != nil {
		return nil, fmt.Errorf("failed to read raw manifest for image %q: %w", imageName, err)
	}

	manifestDigest, err := img.Digest()
	if err != nil {
		return nil, fmt.Errorf("failed to compute manifest digest for image %q: %w", imageName, err)
	}

	configBytes, err := img.RawConfigFile()
	if err != nil {
		return nil, fmt.Errorf("failed to read config for image %q: %w", imageName, err)
	}

	layers, err := img.Layers()
	if err != nil {
		return nil, fmt.Errorf("failed to read layers for image %q: %w", imageName, err)
	}

	if len(layers) != len(manifest.Layers) {
		return nil, fmt.Errorf("manifest for image %q describes %d layers but %d layers were loaded", imageName, len(manifest.Layers), len(layers))
	}

	return &imageArtifacts{
		manifest:       manifest,
		manifestBytes:  manifestBytes,
		manifestDigest: manifestDigest.String(),
		manifestSize:   int64(len(manifestBytes)),
		configBytes:    configBytes,
		configDigest:   manifest.Config.Digest.String(),
		configSize:     manifest.Config.Size,
		layers:         layers,
	}, nil
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

// Write saves the Docker image layers and manifest into the cache using go-containerregistry
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

	artifacts, err := collectImageArtifacts(imageName, img)
	if err != nil {
		return nil, err
	}

	configSize := artifacts.configSize
	if configSize == 0 {
		configSize = int64(len(artifacts.configBytes))
	}

	totalBytes := artifacts.manifestSize + configSize
	layerDigests := make([]*gen.Digest, 0, len(artifacts.manifest.Layers))
	for _, layer := range artifacts.manifest.Layers {
		totalBytes += layer.Size
		layerDigests = append(layerDigests, &gen.Digest{Hash: layer.Digest.String(), SizeBytes: layer.Size})
	}

	progress := tracker
	if progress != nil {
		progress = progress.SubTracker(
			fmt.Sprintf("%s: writing docker image %s", target.Label, imageName),
			totalBytes,
		)
	}

	// Write config blob
	configReader := bytes.NewReader(artifacts.configBytes)
	var reader io.Reader = configReader
	if progress != nil {
		reader = progress.WrapReader(configReader)
	}
	if err := d.cas.Write(ctx, artifacts.configDigest, reader); err != nil {
		return nil, fmt.Errorf("failed to write config blob for image %s: %w", imageName, err)
	}

	// Write each layer individually
	for idx, layer := range artifacts.layers {
		descriptor := artifacts.manifest.Layers[idx]
		layerReader, err := layer.Compressed()
		if err != nil {
			return nil, fmt.Errorf("failed to open layer %d for image %s: %w", idx, imageName, err)
		}

		reader = layerReader
		if progress != nil {
			reader = progress.WrapReader(layerReader)
		}

		if err := d.cas.Write(ctx, descriptor.Digest.String(), reader); err != nil {
			layerReader.Close()
			return nil, fmt.Errorf("failed to write layer %s to cache: %w", descriptor.Digest, err)
		}

		if err := layerReader.Close(); err != nil {
			return nil, fmt.Errorf("failed to close layer reader for %s: %w", descriptor.Digest, err)
		}
	}

	// Write manifest blob
	manifestReader := bytes.NewReader(artifacts.manifestBytes)
	reader = manifestReader
	if progress != nil {
		reader = progress.WrapReader(manifestReader)
	}
	if err := d.cas.Write(ctx, artifacts.manifestDigest, reader); err != nil {
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
				TarDigest:      artifacts.manifestDigest,
				ManifestDigest: &gen.Digest{Hash: artifacts.manifestDigest, SizeBytes: artifacts.manifestSize},
				ConfigDigest:   &gen.Digest{Hash: artifacts.configDigest, SizeBytes: configSize},
				LayerDigests:   layerDigests,
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
		if existingDigest, err := existingImg.Digest(); err == nil && manifestDigest != nil && existingDigest.String() == manifestDigest.GetHash() {
			logger.Debugf("image %s already exists locally so skipping load", imageName)
			return nil
		}
	}

	manifestBytes, err := d.cas.LoadBytes(ctx, manifestDigest.GetHash())
	if err != nil {
		return fmt.Errorf("failed to read manifest for image %s from cache: %w", imageName, err)
	}

	manifest, err := v1.ParseManifest(bytes.NewReader(manifestBytes))
	if err != nil {
		return fmt.Errorf("failed to parse manifest for image %s: %w", imageName, err)
	}

	if len(manifest.Layers) != len(dockerImage.GetLayerDigests()) {
		return fmt.Errorf("cached manifest for %s has %d layers but output recorded %d", imageName, len(manifest.Layers), len(dockerImage.GetLayerDigests()))
	}

	manifestSize := manifestDigest.GetSizeBytes()
	if manifestSize == 0 {
		manifestSize = int64(len(manifestBytes))
	}

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

	layerDigests := dockerImage.GetLayerDigests()
	layers := make([]v1.Layer, 0, len(layerDigests))
	for idx, layerDigest := range layerDigests {
		descriptor := manifest.Layers[idx]
		if descriptor.Digest.String() != layerDigest.GetHash() {
			return fmt.Errorf("layer digest mismatch for image %s: manifest has %s but output recorded %s", imageName, descriptor.Digest, layerDigest.GetHash())
		}

		opener := func() (io.ReadCloser, error) {
			reader, err := d.cas.Load(ctx, layerDigest.GetHash())
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

		layer, err := tarball.LayerFromOpener(opener, tarball.WithMediaType(descriptor.MediaType))
		if err != nil {
			return fmt.Errorf("failed to reconstruct layer %s for image %s: %w", descriptor.Digest, imageName, err)
		}

		layers = append(layers, layer)
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
