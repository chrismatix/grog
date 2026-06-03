package handlers

import (
	"context"
	"fmt"
	"sync/atomic"

	"grog/internal/console"
	"grog/internal/model"
	"grog/internal/proto/gen"
	"grog/internal/worker"
)

// PushFunc performs the actual registry-to-registry copy. It is injected at
// construction time so the handlers package stays import-light: the
// go-containerregistry dependency lives in internal/ocipush and is wired up
// by the output registry, not here.
//
// The function must HEAD-probe the destination, perform the copy with retries
// on transient errors, and return nil when the destination already holds an
// image whose digest matches the source.
type PushFunc func(ctx context.Context, source, destination string, sourceInsecure bool) error

// OciPushOutputHandler implements the oci-push:: output type by delegating its cache
// behavior (Hash/Write/Load) to the underlying docker handler — whichever one
// the cache backend selects — and adding a registry-to-registry copy on top
// when --push is enabled. The cache side stays unchanged from docker::, which
// is exactly the DRY property we want: one cache implementation, two output
// types that compose on top of it.
//
// The handler holds:
//
//   - inner:   the docker handler driving the cache (one of DockerOutputHandler
//     or DockerRegistryOutputHandler, exposed as Handler).
//   - source:  the same instance, type-asserted to DockerImageSource so the
//     push plan can resolve the cache reference at Execute time
//     (after the inner cache plan has populated digests on the proto).
//   - pushFn:  ocipush.Copy, injected to keep this package import-light.
//   - reporter: accumulates per-push results for the build summary.
//   - pushEnabled: closure over config.Global.Push so test code can flip it
//     without mutating package globals.
type OciPushOutputHandler struct {
	inner       Handler
	source      DockerImageSource
	pushFn      PushFunc
	reporter    *PushReporter
	pushEnabled func() bool
	failFast    func() bool

	// aborted is set by the first push plan that fails while --fail-fast is
	// on. Subsequent push plans observe it at the top of Execute and bail
	// without performing a network round-trip. This does not cancel pushes
	// already in flight — those run to completion (success or failure) and
	// record their own outcome — but it stops the pipeline from chewing
	// through N more failures in series after the first.
	aborted atomic.Bool
}

// NewOciPushOutputHandler wires the OciPushOutputHandler around an inner docker handler.
// The inner must implement DockerImageSource; otherwise the handler can't
// produce a source ref for the registry-to-registry copy and construction
// panics — this is a programming error caught at startup, not a runtime
// condition.
func NewOciPushOutputHandler(
	inner Handler,
	pushFn PushFunc,
	reporter *PushReporter,
	pushEnabled func() bool,
	failFast func() bool,
) *OciPushOutputHandler {
	source, ok := inner.(DockerImageSource)
	if !ok {
		panic(fmt.Sprintf("inner handler %T does not implement DockerImageSource — cannot wire oci-push", inner))
	}
	if failFast == nil {
		failFast = func() bool { return false }
	}
	return &OciPushOutputHandler{
		inner:       inner,
		source:      source,
		pushFn:      pushFn,
		reporter:    reporter,
		pushEnabled: pushEnabled,
		failFast:    failFast,
	}
}

func (h *OciPushOutputHandler) Type() HandlerType { return OciPushHandler }

// Hash delegates to the inner docker handler. The identifier the inner handler
// sees is the same string — for oci-push:: the recipe produces a local tag
// whose name equals the remote push tag (option A from the design grilling),
// so `output.Identifier` is a valid local-tag input for the inner handler.
func (h *OciPushOutputHandler) Hash(ctx context.Context, target model.Target, output model.Output) (string, error) {
	return h.inner.Hash(ctx, target, asDockerOutput(output))
}

// Load delegates to the inner handler: oci-push:: outputs restore from cache
// the same way docker:: outputs do. Push is purely a write-side concern.
func (h *OciPushOutputHandler) Load(ctx context.Context, target model.Target, output *gen.Output, tracker *worker.ProgressTracker) error {
	return h.inner.Load(ctx, target, output, tracker)
}

// Write runs the inner handler's Write to stage the cache plan, stamps the
// resulting proto with push_destination so cache round-trips preserve the
// oci-push semantics, and — when --push is enabled — appends a push write
// plan that ships the cached image to the user's destination.
func (h *OciPushOutputHandler) Write(
	ctx context.Context,
	target model.Target,
	output model.Output,
	tracker *worker.ProgressTracker,
) (*PreparedOutput, error) {
	prepared, err := h.inner.Write(ctx, target, asDockerOutput(output), tracker)
	if err != nil {
		return nil, err
	}

	dockerOut := prepared.Output.GetDockerImage()
	if dockerOut == nil {
		return nil, fmt.Errorf("inner handler %T returned non-docker output for oci-push:: input", h.inner)
	}
	dockerOut.PushDestination = output.Identifier

	if !h.pushEnabled() {
		return prepared, nil
	}

	pushPlan := &ociPushWritePlan{
		pushFn:      h.pushFn,
		source:      h.source,
		dockerOut:   dockerOut,
		destination: output.Identifier,
		targetLabel: target.Label.String(),
		reporter:    h.reporter,
		aborted:     &h.aborted,
		failFast:    h.failFast,
	}

	// Chain the cache plan and the push plan: the push reads digests the
	// cache plan stamps onto dockerOut, so order is load-bearing.
	prepared.WritePlan = &CompositeWritePlan{
		Plans: []OutputWritePlan{prepared.WritePlan, pushPlan},
	}
	return prepared, nil
}

// asDockerOutput rewrites an oci-push:: identifier as a docker:: input for the
// inner handler. Translation is the identity: option A means the local tag
// the recipe produced equals the push destination, so the inner handler can
// look it up via `ImageInspect(<destination>)` exactly as it would for a
// vanilla docker:: output.
func asDockerOutput(out model.Output) model.Output {
	return model.NewOutput(string(DockerHandler), out.Identifier)
}

// ociPushWritePlan ships the cached image to its user-facing destination using
// the injected PushFunc. It runs strictly after the inner cache plan: the
// CompositeWritePlan's Execute orders them.
type ociPushWritePlan struct {
	pushFn   PushFunc
	source   DockerImageSource
	reporter *PushReporter

	// dockerOut is the same pointer the cache plan mutates with image_id /
	// manifest_digest. Reading it here resolves the source ref using whatever
	// values the cache plan populated.
	dockerOut *gen.DockerImageOutput

	destination string
	targetLabel string

	// aborted is the handler-wide abort flag flipped by the first
	// fail-fast failure. Pointer (not value) so all plans share state.
	aborted  *atomic.Bool
	failFast func() bool
}

func (p *ociPushWritePlan) Execute(ctx context.Context, tracker *worker.ProgressTracker) error {
	logger := console.GetLogger(ctx)

	// Short-circuit when an earlier fail-fast push has already failed. The
	// failure is recorded so the build summary distinguishes "skipped due to
	// abort" from "actively succeeded" — both surface as non-zero exit but
	// only the originating push gets attributed the cause.
	if p.aborted.Load() {
		err := fmt.Errorf("aborted after earlier push failure (--fail-fast)")
		p.reporter.Record(PushReport{
			TargetLabel: p.targetLabel,
			Destination: p.destination,
			Err:         err,
		})
		return err
	}

	tracker.SetStatus(fmt.Sprintf("%s: pushing %s", p.targetLabel, p.destination))

	sourceRef, insecure, ok := p.source.SourceRef(p.dockerOut)
	if !ok {
		err := fmt.Errorf("%s: cache write did not populate a source reference for push to %s", p.targetLabel, p.destination)
		p.reporter.Record(PushReport{
			TargetLabel: p.targetLabel,
			Destination: p.destination,
			Err:         err,
		})
		p.maybeAbort()
		return err
	}

	err := p.pushFn(ctx, sourceRef, p.destination, insecure)
	report := PushReport{
		TargetLabel: p.targetLabel,
		Destination: p.destination,
		Err:         err,
	}
	p.reporter.Record(report)
	if err != nil {
		logger.Warnf("%s: push to %s failed: %v", p.targetLabel, p.destination, err)
		p.maybeAbort()
		return err
	}
	logger.Debugf("%s: pushed %s", p.targetLabel, p.destination)
	return nil
}

// maybeAbort flips the shared abort flag when --fail-fast is on so subsequent
// push plans short-circuit instead of grinding through their own failures.
func (p *ociPushWritePlan) maybeAbort() {
	if p.failFast != nil && p.failFast() {
		p.aborted.Store(true)
	}
}

func (p *ociPushWritePlan) Cleanup(_ context.Context) error {
	// The push plan owns no daemon-side state — bytes move registry-to-
	// registry without a local tag round-trip — so there's nothing to clean
	// up. Local-tag cleanup of the recipe's own tag is intentionally skipped
	// per the design (Q9 (A): leave it).
	return nil
}
