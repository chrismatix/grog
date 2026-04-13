package handlers

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"

	dockerconfig "github.com/docker/cli/cli/config"
	"github.com/docker/docker/api/types/image"
	"github.com/docker/docker/errdefs"
	"github.com/docker/docker/pkg/jsonmessage"

	"grog/internal/caching"
	"grog/internal/config"
	"grog/internal/console"
	"grog/internal/model"
	"grog/internal/proto/gen"
	"grog/internal/worker"

	"github.com/docker/docker/client"
)

// DockerRegistryOutputHandler writes Docker images to and loads them from a registry specified by configuration.
type DockerRegistryOutputHandler struct {
	cas          *caching.Cas
	config       config.DockerConfig
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
	if err != nil && !errdefs.IsNotFound(err) {
		console.GetLogger(ctx).Warnf("failed to remove image %q from local Docker daemon: %v", d.remoteImageName, err)
	}
	return nil
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

	// strip the leading sha256: prefix from the digest
	if strings.Contains(digest, ":") {
		digest = strings.Split(digest, ":")[1]
	}

	return fmt.Sprintf("%s/%s-%s", d.config.Registry,
		workspacePrefix, digest)
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

	remoteCacheImageName := d.cacheImageName(inspect.ID)

	logger.Debugf("tagging Docker image %s as %s for deferred push", localImageName, remoteCacheImageName)

	if err := cli.ImageTag(ctx, localImageName, remoteCacheImageName); err != nil {
		return nil, fmt.Errorf("failed to tag image %q as %q: %w", localImageName, remoteCacheImageName, err)
	}

	genOutput := &gen.Output{
		Kind: &gen.Output_DockerImage{
			DockerImage: &gen.DockerImageOutput{
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

	return &PreparedOutput{
		Output:    genOutput,
		WritePlan: writePlan,
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
func (d *DockerRegistryOutputHandler) Load(
	ctx context.Context,
	target model.Target,
	output *gen.Output,
	tracker *worker.ProgressTracker,
) error {
	dockerImage := output.GetDockerImage()
	if dockerImage.GetMode() != gen.ImageMode_REGISTRY {
		return fmt.Errorf("cannot restore %s docker cache as registry cache is configured", dockerImage.GetMode())
	}
	localImageName := output.GetDockerImage().GetLocalTag()
	imageId := output.GetDockerImage().GetImageId()

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

	// Read and bridge the Docker JSON progress from pull before tagging
	if err := consumeDockerProgress(pull, tracker, fmt.Sprintf("%s: pulling cache for %s", target.Label, localImageName)); err != nil {
		return fmt.Errorf("error reading pull response: %w", err)
	}

	if err := cli.ImageTag(ctx, remoteImageName, localImageName); err != nil {
		return fmt.Errorf("failed to tag cache image %q as %q: %w", remoteImageName, localImageName, err)
	}

	logger.Debugf("successfully loaded Docker image %s from registry tag %s", localImageName, remoteImageName)
	logger.Infof("Loaded image %s from registry", localImageName)
	return nil
}

// dockerLayerProgress holds per-layer tracking state for bridging JSON progress
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
//	"(3 pushing, 1 preparing, 1 cached)"
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
	return fmt.Sprintf("(%s)", strings.Join(parts, ", "))
}

// formatLayerPhaseSummary builds a human-readable summary with the base status
// prefix. Used for logging and non-UI contexts.
func formatLayerPhaseSummary(base string, layerStates map[string]string) string {
	summary := formatPhaseSummary(layerStates)
	if summary == "" {
		return base
	}
	return fmt.Sprintf("%s %s", base, summary)
}
