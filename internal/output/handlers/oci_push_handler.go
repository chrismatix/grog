package handlers

import (
	"context"
	"fmt"

	"grog/internal/console"
	"grog/internal/model"
	"grog/internal/proto/gen"
	"grog/internal/worker"
)

// OciPushOutputHandler wraps an oci cache handler and adds a registry-to-
// registry push when --push is set. It is registered under "oci-push" and
// delegates Hash/Write/Load to the inner handler; only the push-related
// hooks live here.
type OciPushOutputHandler struct {
	inner        Handler
	pusher       ImagePusher
	pushReporter *PushReporter
	pushEnabled  func() bool
}

// NewOciPushOutputHandler returns a wrapper over an oci cache handler. The
// inner handler must implement ImagePusher; both backends (fs loopback and
// remote registry) do.
func NewOciPushOutputHandler(inner Handler, reporter *PushReporter, pushEnabled func() bool) *OciPushOutputHandler {
	pusher, ok := inner.(ImagePusher)
	if !ok {
		panic(fmt.Sprintf("inner handler %T does not implement ImagePusher", inner))
	}
	if pushEnabled == nil {
		pushEnabled = func() bool { return false }
	}
	return &OciPushOutputHandler{
		inner:        inner,
		pusher:       pusher,
		pushReporter: reporter,
		pushEnabled:  pushEnabled,
	}
}

func (h *OciPushOutputHandler) Type() HandlerType { return OciPushHandler }

func (h *OciPushOutputHandler) Hash(ctx context.Context, target model.Target, output model.Output) (string, error) {
	return h.inner.Hash(ctx, target, asOciOutput(output))
}

// Write delegates the cache write to the inner handler under an oci:: input,
// stamps push_destination on the proto so the cached TargetResult round-trips
// oci-push semantics, and — when --push is on — chains an ociPushPlan behind
// the cache plan.
func (h *OciPushOutputHandler) Write(ctx context.Context, target model.Target, output model.Output, tracker *worker.ProgressTracker) (*PreparedOutput, error) {
	prepared, err := h.inner.Write(ctx, target, asOciOutput(output), tracker)
	if err != nil {
		return nil, err
	}
	image := prepared.Output.GetOciImage()
	if image == nil {
		return nil, fmt.Errorf("inner handler %T returned non-oci output for oci-push:: input", h.inner)
	}
	image.PushDestination = output.Identifier
	if !h.pushEnabled() {
		return prepared, nil
	}
	prepared.WritePlan = &CompositeWritePlan{Plans: []OutputWritePlan{
		prepared.WritePlan,
		newOciPushPlan(h.pusher, image, output.Identifier, target.Label.String(), h.pushReporter),
	}}
	return prepared, nil
}

// Load restores the cached image via the inner handler, then fires the same
// push the write path would. Push errors are recorded to the reporter but
// not returned — a transient push failure must not invalidate a successful
// cache restore.
func (h *OciPushOutputHandler) Load(ctx context.Context, target model.Target, output *gen.Output, tracker *worker.ProgressTracker) error {
	if err := h.inner.Load(ctx, target, output, tracker); err != nil {
		return err
	}
	if !h.pushEnabled() {
		return nil
	}
	image := output.GetOciImage()
	if image == nil || image.GetPushDestination() == "" {
		return nil
	}
	_ = newOciPushPlan(h.pusher, image, image.GetPushDestination(), target.Label.String(), h.pushReporter).Execute(ctx, tracker)
	return nil
}

// asOciOutput translates an oci-push:: input into an oci:: input. Option A:
// the local tag the recipe produced equals the push destination, so the
// inner handler can look it up via ImageInspect(<destination>) exactly as
// it would for a vanilla oci:: output.
func asOciOutput(out model.Output) model.Output {
	return model.NewOutput(string(OCIHandler), out.Identifier)
}

// ociPushPlan ships the cached image to its user-facing destination by
// invoking the inner handler's PushImage. When chained behind a cache write
// plan, the cache plan has already populated image_id / manifest_digest on
// dockerOut by the time Execute runs.
type ociPushPlan struct {
	pusher      ImagePusher
	dockerOut   *gen.OCIImageOutput
	destination string
	targetLabel string
	reporter    *PushReporter
}

func newOciPushPlan(pusher ImagePusher, image *gen.OCIImageOutput, destination, targetLabel string, reporter *PushReporter) *ociPushPlan {
	return &ociPushPlan{
		pusher:      pusher,
		dockerOut:   image,
		destination: destination,
		targetLabel: targetLabel,
		reporter:    reporter,
	}
}

func (p *ociPushPlan) Execute(ctx context.Context, tracker *worker.ProgressTracker) error {
	logger := console.GetLogger(ctx)

	if p.reporter.Aborted() {
		err := fmt.Errorf("aborted after earlier push failure (--fail-fast)")
		p.reporter.Record(PushReport{TargetLabel: p.targetLabel, Destination: p.destination, Err: err})
		return err
	}

	tracker.SetStatus(fmt.Sprintf("%s: pushing %s", p.targetLabel, p.destination))

	skipped, err := p.pusher.PushImage(ctx, p.dockerOut, p.destination, tracker)
	p.reporter.Record(PushReport{
		TargetLabel: p.targetLabel,
		Destination: p.destination,
		Skipped:     skipped,
		Err:         err,
	})
	if err != nil {
		logger.Warnf("%s: push to %s failed: %v", p.targetLabel, p.destination, err)
	}
	return err
}

func (p *ociPushPlan) Cleanup(_ context.Context) error { return nil }
