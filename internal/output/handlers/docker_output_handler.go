package handlers

import (
	"context"
	"encoding/base64"
	"fmt"
	"strings"
	"sync"

	"grog/internal/caching"
	"grog/internal/console"
	"grog/internal/dockerproxy"
	"grog/internal/model"
	"grog/internal/proto/gen"
	"grog/internal/worker"

	"github.com/docker/docker/api/types/image"
	"github.com/docker/docker/client"
	"github.com/docker/docker/errdefs"
)

// DockerOutputHandler caches docker images by pushing them to an in-process
// OCI Distribution registry that proxies blob/manifest writes into grog's CAS.
//
// The Docker daemon does the actual pushing — streaming compressed layers from
// its content store directly to our loopback HTTP endpoint, which forwards them
// to the configured remote cache backend (S3/GCS/Azure/disk). Grog never holds
// image bytes in its own memory and never has to recompress layers, so the
// throughput matches what `docker push` to a real registry would achieve.
type DockerOutputHandler struct {
	cas *caching.Cas

	// dockerClient is created lazily on first use; sync.Once protects construction.
	dockerInit   sync.Once
	dockerClient *client.Client
	dockerErr    error

	// proxy is the loopback OCI registry. Lazily started on first push so that
	// runs without docker targets pay no cost.
	proxyInit sync.Once
	proxy     *dockerproxy.Registry
	proxyErr  error
}

// NewDockerOutputHandler creates a new DockerOutputHandler. The Docker client
// and the in-process registry are both created lazily on first use.
func NewDockerOutputHandler(_ context.Context, cas *caching.Cas) *DockerOutputHandler {
	return &DockerOutputHandler{cas: cas}
}

// Type returns the type of the handler.
func (d *DockerOutputHandler) Type() HandlerType {
	return DockerHandler
}

func (d *DockerOutputHandler) ensureClient() (*client.Client, error) {
	d.dockerInit.Do(func() {
		cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
		if err != nil {
			d.dockerErr = fmt.Errorf("failed to create docker client: %w", err)
			return
		}
		d.dockerClient = cli
	})
	return d.dockerClient, d.dockerErr
}

func (d *DockerOutputHandler) ensureProxy(ctx context.Context) (*dockerproxy.Registry, error) {
	d.proxyInit.Do(func() {
		reg, err := dockerproxy.New(ctx, d.cas)
		if err != nil {
			d.proxyErr = fmt.Errorf("failed to start in-process docker registry proxy: %w", err)
			return
		}
		d.proxy = reg
	})
	return d.proxy, d.proxyErr
}

// Hash returns a stable identifier for the local Docker image. We use the
// Docker image ID (the digest of the image config) which fully identifies the
// image content and matches what DockerRegistryOutputHandler uses, so the two
// backends are interchangeable from a caching standpoint.
func (d *DockerOutputHandler) Hash(ctx context.Context, _ model.Target, output model.Output) (string, error) {
	cli, err := d.ensureClient()
	if err != nil {
		return "", err
	}

	imageName := output.Identifier
	console.GetLogger(ctx).Debugf("hashing Docker image %s via local daemon", imageName)

	inspect, err := cli.ImageInspect(ctx, imageName)
	if err != nil {
		return "", fmt.Errorf("failed to inspect image %q: %w", imageName, err)
	}
	return inspect.ID, nil
}

// Write tags the local image with a loopback registry reference and stages a
// write plan that will push it via cli.ImagePush during the cache-write phase.
// No image bytes are read in this method — the heavy lifting is deferred until
// Execute, when the daemon streams the layers itself.
func (d *DockerOutputHandler) Write(
	ctx context.Context,
	target model.Target,
	output model.Output,
	_ *worker.ProgressTracker,
) (*PreparedOutput, error) {
	logger := console.GetLogger(ctx)
	imageName := output.Identifier

	cli, err := d.ensureClient()
	if err != nil {
		return nil, err
	}
	proxy, err := d.ensureProxy(ctx)
	if err != nil {
		return nil, err
	}

	inspect, err := cli.ImageInspect(ctx, imageName)
	if err != nil {
		return nil, fmt.Errorf("%s: image output %s was not created: %w", target.Label.String(), imageName, err)
	}

	repoName := loopbackRepoName(inspect.ID)
	loopbackRef := fmt.Sprintf("%s/%s:%s", proxy.Addr(), repoName, shortID(inspect.ID))

	logger.Debugf("tagging Docker image %s as %s for loopback push", imageName, loopbackRef)
	if err := cli.ImageTag(ctx, imageName, loopbackRef); err != nil {
		return nil, fmt.Errorf("failed to tag image %q as %q: %w", imageName, loopbackRef, err)
	}

	dockerImageOutput := &gen.DockerImageOutput{
		Mode:     gen.ImageMode_LAYERS,
		LocalTag: imageName,
		ImageId:  inspect.ID,
		// ManifestDigest is filled in by Execute once the daemon has produced one.
	}

	genOutput := &gen.Output{
		Kind: &gen.Output_DockerImage{
			DockerImage: dockerImageOutput,
		},
	}

	plan := &dockerImageWritePlan{
		dockerClient:   cli,
		proxy:          proxy,
		output:         dockerImageOutput,
		loopbackRef:    loopbackRef,
		repoName:       repoName,
		localImageName: imageName,
		targetLabel:    target.Label.String(),
	}

	return &PreparedOutput{
		Output:    genOutput,
		WritePlan: plan,
	}, nil
}

// Load restores a previously cached image into the local Docker daemon by
// pulling it from the loopback registry by manifest digest. The daemon does
// the actual layer download — grog only translates the cached digests into
// a registry reference and tags the result.
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

	logger := console.GetLogger(ctx)
	localImageName := dockerImage.GetLocalTag()
	imageID := dockerImage.GetImageId()
	manifestDigest := dockerImage.GetManifestDigest().GetHash()

	if manifestDigest == "" {
		return fmt.Errorf("cached docker image %q has no manifest digest", localImageName)
	}

	cli, err := d.ensureClient()
	if err != nil {
		return err
	}
	proxy, err := d.ensureProxy(ctx)
	if err != nil {
		return err
	}

	// Fast path: the image content is already present in the daemon. Just make
	// sure the requested local tag points at it.
	if imageID != "" {
		if _, err := cli.ImageInspect(ctx, imageID); err == nil {
			if err := cli.ImageTag(ctx, imageID, localImageName); err != nil {
				return fmt.Errorf("failed to tag existing image %q as %q: %w", imageID, localImageName, err)
			}
			logger.Debugf("image %s already present locally, skipped pull", localImageName)
			return nil
		}
	}

	repoName := loopbackRepoName(imageID)
	pullRef := fmt.Sprintf("%s/%s@%s", proxy.Addr(), repoName, manifestDigest)

	logger.Debugf("pulling Docker image %s from loopback registry", pullRef)
	pullReader, err := cli.ImagePull(ctx, pullRef, image.PullOptions{
		RegistryAuth: emptyRegistryAuth,
	})
	if err != nil {
		return fmt.Errorf("failed to pull image %q from loopback registry: %w", pullRef, err)
	}
	defer pullReader.Close()

	if err := consumeDockerProgress(pullReader, tracker, fmt.Sprintf("%s: pulling cache for %s", target.Label, localImageName)); err != nil {
		return fmt.Errorf("error reading pull response: %w", err)
	}

	// After pull, the daemon stores the image under the loopback @digest reference.
	// Re-tag it as the original local image name and discard the loopback ref.
	if err := cli.ImageTag(ctx, pullRef, localImageName); err != nil {
		return fmt.Errorf("failed to tag pulled image %q as %q: %w", pullRef, localImageName, err)
	}
	if _, err := cli.ImageRemove(ctx, pullRef, image.RemoveOptions{}); err != nil && !errdefs.IsNotFound(err) {
		logger.Warnf("failed to remove loopback ref %q from local daemon: %v", pullRef, err)
	}

	logger.Debugf("successfully loaded Docker image %s from cache", localImageName)
	logger.Infof("Loaded image %s from cache backend", localImageName)
	return nil
}

// dockerImageWritePlan defers the actual `docker push` to the cache-write phase.
// Execute pushes to the loopback registry, then mutates the embedded Output proto
// to record the manifest digest the daemon produced — that's how Load later knows
// which manifest to pull.
type dockerImageWritePlan struct {
	dockerClient *client.Client
	proxy        *dockerproxy.Registry

	// output is the same pointer that lives in PreparedTargetResult.TargetResult.Outputs[i].
	// Execute mutates its ManifestDigest field; the cache writer persists the
	// proto AFTER all write plans succeed, so the mutation is captured.
	output *gen.DockerImageOutput

	loopbackRef    string // host:port/repo:tag — the local tag we put on the image
	repoName       string // path part of loopbackRef, used to look up the manifest digest
	localImageName string
	targetLabel    string
}

func (d *dockerImageWritePlan) Execute(ctx context.Context, tracker *worker.ProgressTracker) error {
	logger := console.GetLogger(ctx)

	baseStatus := fmt.Sprintf("%s: caching docker image %s", d.targetLabel, d.localImageName)
	// Surface the plan's phase immediately so the UI stops saying
	// "writing cache" before the daemon has sent its first JSON message. For
	// big overlay2-backed images the gzip/prepare phase alone can take
	// several minutes, and the user should see *something* during that wait.
	tracker.SetStatus(baseStatus)

	logger.Debugf("pushing Docker image %s to loopback registry", d.loopbackRef)
	pushReader, err := d.dockerClient.ImagePush(ctx, d.loopbackRef, image.PushOptions{
		RegistryAuth: emptyRegistryAuth,
	})
	if err != nil {
		return fmt.Errorf("failed to push image %q to loopback registry: %w", d.loopbackRef, err)
	}
	defer pushReader.Close()

	if err := consumeDockerProgress(pushReader, tracker, baseStatus); err != nil {
		return fmt.Errorf("error reading push response: %w", err)
	}

	// The stream has drained but the plan still has finalisation work (reading
	// the manifest digest back and mutating the output proto). Call it out so
	// the user knows we've moved on from the daemon push itself.
	tracker.SetStatus(baseStatus + ": finalizing")

	manifestDigest := d.proxy.LastManifestDigest(d.repoName)
	if manifestDigest == "" {
		return fmt.Errorf("docker push for %q completed but the loopback registry never received a manifest", d.loopbackRef)
	}
	d.output.ManifestDigest = &gen.Digest{Hash: manifestDigest}

	logger.Debugf("successfully cached Docker image %s (manifest %s)", d.localImageName, manifestDigest)
	return nil
}

// Close shuts down the in-process loopback registry, if one was started.
// Must be called only after all async cache writes that push through the
// proxy have drained — otherwise the daemon's mid-push HTTP requests will
// fail with "connection refused" once the listener is torn down.
func (d *DockerOutputHandler) Close() error {
	if d.proxy == nil {
		return nil
	}
	return d.proxy.Close()
}

func (d *dockerImageWritePlan) Cleanup(ctx context.Context) error {
	if _, err := d.dockerClient.ImageRemove(ctx, d.loopbackRef, image.RemoveOptions{}); err != nil && !errdefs.IsNotFound(err) {
		console.GetLogger(ctx).Warnf("failed to remove loopback tag %q: %v", d.loopbackRef, err)
	}
	return nil
}

// loopbackRepoName builds a repository path component from an image ID. The
// path is stable for a given image so concurrent pushes of the same content
// converge on the same name (which is fine — the manifest digest they record
// is identical).
func loopbackRepoName(imageID string) string {
	return "grog-cache/" + shortID(imageID)
}

// shortID strips the sha256: prefix from a digest and returns the first 32
// hex characters. Long enough to be effectively unique within a build, short
// enough to keep log lines readable.
func shortID(imageID string) string {
	id := strings.TrimPrefix(imageID, "sha256:")
	if len(id) > 32 {
		id = id[:32]
	}
	if id == "" {
		id = "unknown"
	}
	return id
}

// emptyRegistryAuth is a base64-encoded empty AuthConfig. The Docker daemon
// requires *some* RegistryAuth header on push/pull even when the target is a
// non-authenticated localhost registry; an empty JSON object satisfies it.
var emptyRegistryAuth = base64.URLEncoding.EncodeToString([]byte("{}"))
