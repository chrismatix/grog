package handlers

import (
	"context"
	"fmt"

	"grog/internal/console"
	"grog/internal/proto/gen"
	"grog/internal/worker"
)

// ociPushPlan ships the cached image to its user-facing destination by
// invoking the docker handler's own PushImage. It runs after the cache write
// plan it is chained behind, by which point the cache plan has populated
// image_id / manifest_digest on dockerOut.
type ociPushPlan struct {
	pusher      ImagePusher
	dockerOut   *gen.DockerImageOutput
	destination string
	targetLabel string
	reporter    *PushReporter
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
