package handlers

import (
	"context"
	"fmt"

	"grog/internal/console"
	"grog/internal/proto/gen"
	"grog/internal/worker"
)

// OciPushPlan ships a cached image to a user-facing destination via the oci
// handler's PushImage.
type OciPushPlan struct {
	pusher      ImagePusher
	dockerOut   *gen.OCIImageOutput
	destination string
	targetLabel string
	reporter    *PushReporter
}

func NewOciPushPlan(pusher ImagePusher, image *gen.OCIImageOutput, destination, targetLabel string, reporter *PushReporter) *OciPushPlan {
	return &OciPushPlan{
		pusher:      pusher,
		dockerOut:   image,
		destination: destination,
		targetLabel: targetLabel,
		reporter:    reporter,
	}
}

func (p *OciPushPlan) Execute(ctx context.Context, tracker *worker.ProgressTracker) error {
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

func (p *OciPushPlan) Cleanup(_ context.Context) error { return nil }
