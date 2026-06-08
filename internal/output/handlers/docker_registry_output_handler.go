package handlers

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"

	cerrdefs "github.com/containerd/errdefs"
	dockerconfig "github.com/docker/cli/cli/config"
	"github.com/docker/docker/api/types/image"
	"github.com/docker/docker/pkg/jsonmessage"

	"grog/internal/caching"
	"grog/internal/config"
	"grog/internal/console"
	"grog/internal/model"
	"grog/internal/oci_push"
	"grog/internal/proto/gen"
	"grog/internal/worker"

	"github.com/docker/docker/client"
)

// DockerRegistryOutputHandler writes Docker images to and loads them from a registry specified by configuration.
type DockerRegistryOutputHandler struct {
	cas          *caching.Cas
	config       config.OCIConfig
	dockerClient *client.Client
}

// dockerRegistryWritePlan pushes a pre-tagged Docker image to the configured remote
// registry. The image is tagged with a cache-specific name during Write(); this plan
// pushes that tag and Cleanup removes it from the local daemon to avoid polluting it.
type dockerRegistryWritePlan struct {
	dockerClient    *client.Client
	remoteImageName string
	localImageName  string
	targetLabel     string
}

func (d *dockerRegistryWritePlan) Execute(ctx context.Context, tracker *worker.ProgressTracker) error {
	baseStatus := fmt.Sprintf("%s: pushing cache for %s", d.targetLabel, d.localImageName)
	// Surface the plan's phase immediately so the UI doesn't dwell on
	// "writing cache" while the daemon prepares (gzips, serializes) layers.
	tracker.SetStatus(baseStatus)

	auth, err := makeRegistryAuth(d.remoteImageName)
	if err != nil {
		return err
	}

	pushReader, err := d.dockerClient.ImagePush(ctx, d.remoteImageName, image.PushOptions{
		RegistryAuth: auth,
	})
	if err != nil {
		return fmt.Errorf("failed to push image %q to registry: %w", d.remoteImageName, err)
	}
	defer pushReader.Close()

	if err := consumeDockerProgress(pushReader, tracker, baseStatus); err != nil {
		return fmt.Errorf("error reading push response: %w", err)
	}

	console.GetLogger(ctx).Debugf("successfully pushed Docker image %s to registry", d.remoteImageName)
	return nil
}

func (d *dockerRegistryWritePlan) Cleanup(ctx context.Context) error {
	_, err := d.dockerClient.ImageRemove(ctx, d.remoteImageName, image.RemoveOptions{})
	if err != nil && !cerrdefs.IsNotFound(err) {
		console.GetLogger(ctx).Warnf("failed to remove image %q from local Docker daemon: %v", d.remoteImageName, err)
	}
	return nil
}

// NewDockerRegistryOutputHandler creates a new DockerRegistryOutputHandler.
func NewDockerRegistryOutputHandler(
	cas *caching.Cas,
	cfg config.OCIConfig,
) *DockerRegistryOutputHandler {
	return &DockerRegistryOutputHandler{
		cas:    cas,
		config: cfg,
	}
}

func (d *DockerRegistryOutputHandler) Type() HandlerType {
	return OCIHandler
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

// lazyClient creates a new Docker client on demand.
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

// PushImage copies the cached image from the configured cache registry to
// destination. Auth flows through the ambient docker keychain; either side
// can be tagged plain-HTTP via oci.insecure_registries.
func (d *DockerRegistryOutputHandler) PushImage(ctx context.Context, image *gen.OCIImageOutput, destination string, _ *worker.ProgressTracker) (bool, error) {
	// The cache image reference grog produced in Write() is recorded on the
	// output proto, so push reads it directly rather than re-deriving the name.
	// This keeps push correct regardless of the cache-naming scheme (declared
	// repo vs content-addressed) and survives a cache hit, where the proto is
	// loaded from cache and no fresh name is computed.
	src := image.GetRemoteTag()
	if src == "" {
		return false, fmt.Errorf("cache write did not record a remote tag for push to %s", destination)
	}
	return oci_push.Copy(ctx, src, destination, oci_push.Options{
		SourceInsecure:      matchesInsecureRegistry(src, d.config.InsecureRegistries),
		DestinationInsecure: matchesInsecureRegistry(destination, d.config.InsecureRegistries),
	})
}

// cacheImageName returns the registry reference grog uses to cache the image
// produced by outputIdentifier on target.
//
// Naming is driven by whether the target declares an oci_push destination for
// this output (see model.Target.OciPush). The declared destination's repository
// — not a name synthesised from the target label — is the cache location, so
// repository names are explicit in the BUILD file and enumerable by Terraform
// without reverse-engineering any sanitisation. This mirrors the oci_push
// explicit-destination model (#159).
//
//   - Declared (oci_push present for this output): the cache image lives in the
//     declared repository, tagged by build platform and the target's content
//     hash: "<repo>:<platform>-<changeHash>". The tag is immutable and
//     content-addressed — a changed input (e.g. a lockfile) yields a new
//     ChangeHash and therefore a new tag, so a stale image can never be
//     restored as the cache. The author's deploy tag (e.g. ":<git-sha>") is
//     ignored for caching: only the repository is taken from the declaration.
//   - Not declared: fall back to the content-addressed scheme (one repo per
//     unique image digest), preserving grog's default behaviour.
//
// digest is only consulted in the content-addressed fallback; the declared
// path needs no digest because the tag is keyed on the pre-build ChangeHash.
func (d *DockerRegistryOutputHandler) cacheImageName(target model.Target, outputIdentifier, digest string) string {
	if repo := declaredCacheRepo(target, outputIdentifier); repo != "" {
		// Content-addressed tag, qualified by platform. grog already keys the
		// target change hash by platform (internal/hashing/hash_target.go), so
		// the cache tag is platform-qualified to match and to keep concurrent
		// multi-arch builds of the same target from colliding.
		platformTag := strings.ReplaceAll(config.Global.GetPlatform(), "/", "-")
		return fmt.Sprintf("%s:%s-%s", repo, platformTag, target.ChangeHash)
	}

	// Default: content-addressed (one repo per unique image digest).
	workspaceDir := config.Global.WorkspaceRoot
	workspacePrefix := config.GetWorkspaceCachePrefix(workspaceDir)
	if strings.Contains(digest, ":") {
		digest = strings.Split(digest, ":")[1]
	}
	return fmt.Sprintf("%s/%s-%s", d.config.Registry, workspacePrefix, digest)
}

// declaredCacheRepo returns the repository portion of the oci_push destination
// declared for outputIdentifier on target, or "" if none is declared. The
// destination may carry a deploy tag (e.g. "registry.org/api:${GIT_SHA}"); only
// the repository is returned — grog tags the cache image with its own
// content-addressed scheme. When multiple destinations are declared for one
// output, the first is used as the cache repository.
func declaredCacheRepo(target model.Target, outputIdentifier string) string {
	destinations := target.OciPush[outputIdentifier]
	if len(destinations) == 0 {
		return ""
	}
	return repoWithoutTag(destinations[0])
}

// repoWithoutTag strips a trailing ":tag" from a registry reference, leaving the
// repository (registry host + path). A ":" inside the registry host's port
// (e.g. "localhost:5000/img") is preserved because the split only treats the
// segment after the final "/" as a candidate tag.
func repoWithoutTag(reference string) string {
	slash := strings.LastIndex(reference, "/")
	lastSegment := reference[slash+1:]
	if colon := strings.LastIndex(lastSegment, ":"); colon != -1 {
		return reference[:slash+1] + lastSegment[:colon]
	}
	return reference
}

// Write inspects and tags the Docker image synchronously, deferring the push to the registry.
func (d *DockerRegistryOutputHandler) Write(
	ctx context.Context,
	target model.Target,
	output model.Output,
	_ *worker.ProgressTracker,
) (*PreparedOutput, error) {
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

	remoteCacheImageName := d.cacheImageName(target, output.Identifier, inspect.ID)

	logger.Debugf("tagging Docker image %s as %s for deferred push", localImageName, remoteCacheImageName)

	if err := cli.ImageTag(ctx, localImageName, remoteCacheImageName); err != nil {
		return nil, fmt.Errorf("failed to tag image %q as %q: %w", localImageName, remoteCacheImageName, err)
	}

	genOutput := &gen.Output{
		Kind: &gen.Output_OciImage{
			OciImage: &gen.OCIImageOutput{
				LocalTag:  localImageName,
				RemoteTag: remoteCacheImageName,
				ImageId:   inspect.ID,
				Mode:      gen.ImageMode_REGISTRY,
			},
		},
	}

	writePlan := &dockerRegistryWritePlan{
		dockerClient:    cli,
		remoteImageName: remoteCacheImageName,
		localImageName:  localImageName,
		targetLabel:     target.Label.String(),
	}

	return &PreparedOutput{Output: genOutput, WritePlan: writePlan}, nil
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

// SeedLayerCache pulls the prior cache image for each of the target's declared
// oci_push outputs into the local Docker daemon, so its layers are available
// for reuse during the build. It is called on cache misses, before the target's
// build command executes.
//
// Seeding only applies to outputs whose cache repository is declared via
// oci_push (the same declaration that names the cache image). priorChangeHash
// identifies the content-addressed image the same target produced at an earlier
// git ref; the pulled image is used solely to warm the layer cache and is never
// restored as the build result, so a wrong or missing donor only lowers the
// layer-hit rate. Seeding is best-effort: every failure is logged and swallowed.
func (d *DockerRegistryOutputHandler) SeedLayerCache(
	ctx context.Context,
	target model.Target,
	priorChangeHash string,
	tracker *worker.ProgressTracker,
) error {
	if priorChangeHash == "" {
		// No prior content could be determined (first build, shallow clone, or
		// no seed ref). Nothing to pull.
		return nil
	}

	logger := console.GetLogger(ctx)

	cli, err := d.lazyClient()
	if err != nil {
		return err
	}

	platformTag := strings.ReplaceAll(config.Global.GetPlatform(), "/", "-")
	for _, outputRef := range target.AllOutputs() {
		if outputRef.Type != "oci" {
			continue
		}
		repo := declaredCacheRepo(target, outputRef.Identifier)
		if repo == "" {
			// Output uses the content-addressed cache; there is no stable prior
			// image to seed from.
			continue
		}
		priorImageName := fmt.Sprintf("%s:%s-%s", repo, platformTag, priorChangeHash)
		d.seedFromImage(ctx, logger, cli, target, priorImageName, tracker)
	}
	return nil
}

// seedFromImage pulls a single prior cache image into the local daemon for layer
// reuse. All failures are best-effort: a missing prior image (expected when the
// target is new or unchanged-but-uncached) and any pull/auth error are logged at
// debug level and otherwise ignored.
func (d *DockerRegistryOutputHandler) seedFromImage(
	ctx context.Context,
	logger *console.Logger,
	cli *client.Client,
	target model.Target,
	priorImageName string,
	tracker *worker.ProgressTracker,
) {
	logger.Debugf("seeding Docker layer cache for %s from %s", target.Label, priorImageName)

	auth, err := makeRegistryAuth(priorImageName)
	if err != nil {
		logger.Debugf("failed to get registry auth for layer cache seed: %v", err)
		return
	}

	pull, err := cli.ImagePull(ctx, priorImageName, image.PullOptions{RegistryAuth: auth})
	if err != nil {
		// Not finding a prior image is expected on first build.
		logger.Debugf("no prior image to seed layer cache for %s: %v", target.Label, err)
		return
	}
	defer pull.Close()

	if err := consumeDockerProgress(pull, tracker, fmt.Sprintf("%s: seeding layer cache", target.Label)); err != nil {
		logger.Debugf("error seeding layer cache for %s: %v", target.Label, err)
		return
	}

	logger.Debugf("successfully seeded Docker layer cache for %s from %s", target.Label, priorImageName)
}

// Load pulls the Docker image from the remote registry and writes it into the local Docker daemon.
func (d *DockerRegistryOutputHandler) Load(
	ctx context.Context,
	target model.Target,
	output *gen.Output,
	tracker *worker.ProgressTracker,
) error {
	dockerImage := output.GetOciImage()
	if dockerImage.GetMode() != gen.ImageMode_REGISTRY {
		return fmt.Errorf("cannot restore %s docker cache as registry cache is configured", dockerImage.GetMode())
	}
	localImageName := output.GetOciImage().GetLocalTag()
	imageId := output.GetOciImage().GetImageId()

	logger := console.GetLogger(ctx)

	cli, err := d.lazyClient()
	if err != nil {
		return err
	}

	// check if the image exists in the local Docker daemon
	if _, err = cli.ImageInspect(ctx, imageId); err == nil {
		// The image content exists, but we must ensure the requested tag points to it.
		// If we don't do this, the ID might exist but the tag (localImageName) might be missing.
		if err := cli.ImageTag(ctx, imageId, localImageName); err != nil {
			return fmt.Errorf("failed to tag existing image %q as %q: %w", imageId, localImageName, err)
		}
		logger.Debugf("image %s already exists in local Docker daemon, skipping pull", localImageName)
		return nil
	}

	remoteImageName := output.GetOciImage().GetRemoteTag()
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

	// Read and bridge the Docker JSON progress from pull before tagging
	if err := consumeDockerProgress(pull, tracker, fmt.Sprintf("%s: pulling cache for %s", target.Label, localImageName)); err != nil {
		return fmt.Errorf("error reading pull response: %w", err)
	}

	if err := cli.ImageTag(ctx, remoteImageName, localImageName); err != nil {
		return fmt.Errorf("failed to tag cache image %q as %q: %w", remoteImageName, localImageName, err)
	}

	logger.Debugf("successfully loaded Docker image %s from registry tag %s", localImageName, remoteImageName)
	return nil
}

// dockerLayerProgress holds per-layer tracking state for bridging JSON progress.
type dockerLayerProgress struct {
	tracker     *worker.ProgressTracker
	lastCurrent int64
	total       int64
}

// consumeDockerProgress decodes Docker CLI JSON messages and bridges them into
// our ProgressTracker model. It does two things:
//
//  1. For every layer that emits "Pushing"/"Downloading" messages with a known
//     total, it creates a child sub-tracker and updates it as bytes flow.
//  2. It updates the parent tracker's *status text* (via SetStatus) as the
//     daemon reports non-progress phase transitions like "Preparing",
//     "Waiting", "Pushed" or "Layer already exists". This gives the user a
//     visible signal that work is happening even during phases where the
//     daemon is not shipping bytes through our HTTP endpoint (e.g. gzipping
//     layers on an overlay2-backed daemon, or waiting for our own
//     finishBlobUpload to flush a temp file into the cache).
//
// The base status string is set on the parent immediately so the UI reflects
// the current plan even before the first daemon message arrives. If parent is
// nil, the stream is still drained but no progress is emitted.
func consumeDockerProgress(
	reader io.Reader,
	parent *worker.ProgressTracker,
	status string,
) error {
	if reader == nil {
		return nil
	}

	// Seed the parent status so the UI shows something meaningful immediately,
	// before the daemon has sent its first JSON message.
	parent.SetStatus(status)

	dec := json.NewDecoder(reader)
	layers := make(map[string]*dockerLayerProgress)

	// layerStates tracks the most recent non-empty daemon status for each
	// layer ID. lastPhaseSummary remembers what we last pushed to
	// parent.SetSubStatus so we don't flood the UI with identical updates.
	layerStates := make(map[string]string)
	lastPhaseSummary := ""

	for {
		var jsonMessage jsonmessage.JSONMessage
		if err := dec.Decode(&jsonMessage); err != nil {
			if err == io.EOF {
				break
			}
			return err
		}

		if jsonMessage.Error != nil {
			return jsonMessage.Error
		}

		// Update the plan-level phase summary whenever a layer transitions to
		// a new named state. This runs even for messages that have no
		// Progress field, so "Preparing" and "Waiting" phases become visible.
		// The summary is shown as a sub-status line below the main status.
		if jsonMessage.ID != "" && jsonMessage.Status != "" {
			if layerStates[jsonMessage.ID] != jsonMessage.Status {
				layerStates[jsonMessage.ID] = jsonMessage.Status
				phaseSummary := formatPhaseSummary(layerStates)
				if phaseSummary != lastPhaseSummary {
					parent.SetSubStatus(phaseSummary)
					lastPhaseSummary = phaseSummary
				}
			}
		}

		// Only handle progress-bearing messages for per-layer sub-trackers.
		if jsonMessage.ID == "" || jsonMessage.Progress == nil {
			continue
		}

		current := jsonMessage.Progress.Current
		total := jsonMessage.Progress.Total

		state, ok := layers[jsonMessage.ID]
		if !ok && total > 0 && parent != nil {
			child := parent.SubTracker(status, total)
			if child != nil {
				state = &dockerLayerProgress{tracker: child, total: total}
				layers[jsonMessage.ID] = state
			}
		}

		if state != nil {
			// Guard against out-of-order or resetting currents
			delta := current - state.lastCurrent
			if delta < 0 {
				// treat as absolute if we observe a reset
				delta = current
			}
			if delta > 0 {
				state.tracker.Add(delta)
				state.lastCurrent = current
			}
			if total > 0 && current >= total {
				state.tracker.Complete()
			}
		}
	}

	// Ensure all trackers are completed at end of stream
	for _, st := range layers {
		st.tracker.Complete()
	}
	return nil
}

// layerPhaseLabels maps the verbose docker daemon status strings to short,
// noun-shaped labels that read correctly with any count: "1 cached" /
// "5 cached", "1 pushing" / "5 pushing". The previous version simply
// lowercased the daemon's status, which produced ungrammatical phrases like
// "2 layer already exists".
//
// Order matters: when multiple phases are active we list them in this order
// so the most action-relevant ("pushing") comes first.
var layerPhaseLabels = []struct {
	state string
	label string
}{
	{"Pushing", "pushing"},
	{"Downloading", "downloading"},
	{"Extracting", "extracting"},
	{"Verifying Checksum", "verifying"},
	{"Preparing", "preparing"},
	{"Waiting", "waiting"},
	{"Mounted from", "mounted"},
	{"Pushed", "pushed"},
	{"Download complete", "downloaded"},
	{"Pull complete", "pulled"},
	{"Layer already exists", "cached"},
	{"Already exists", "cached"},
}

// formatPhaseSummary builds a human-readable summary of the phases a
// docker push/pull is currently in, grouped by state. Returns just the
// parenthetical summary intended for rendering as a sub-status line.
//
// Example output with five layers:
//
//	"3 pushing, 1 preparing, 1 cached"
//
// States are grouped in a fixed order so the line stays readable as layers
// transition. Unknown daemon states are appended in alphabetical order.
func formatPhaseSummary(layerStates map[string]string) string {
	if len(layerStates) == 0 {
		return ""
	}
	counts := make(map[string]int)
	for _, state := range layerStates {
		counts[state]++
	}

	seen := make(map[string]bool, len(layerPhaseLabels))
	var parts []string
	for _, ph := range layerPhaseLabels {
		seen[ph.state] = true
		if n := counts[ph.state]; n > 0 {
			parts = append(parts, fmt.Sprintf("%d %s", n, ph.label))
		}
	}
	// Catch-all: any daemon state we don't know about, in stable alphabetical
	// order. We lower-case it but otherwise leave it untouched — these are
	// rare enough that grammatical-perfection isn't worth the mapping table.
	var extras []string
	for state := range counts {
		if !seen[state] {
			extras = append(extras, state)
		}
	}
	sort.Strings(extras)
	for _, state := range extras {
		parts = append(parts, fmt.Sprintf("%d %s", counts[state], strings.ToLower(state)))
	}

	if len(parts) == 0 {
		return ""
	}
	return strings.Join(parts, ", ")
}
