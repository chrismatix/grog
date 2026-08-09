package handlers

import (
	"context"
	"fmt"

	"grog/internal/console"
	"grog/internal/proto/gen"
	"grog/internal/worker"
)

// OciPushPlan ships an image to a user-facing destination via the oci handler.
// The image is sourced either from the cache (NewOciPushPlan) or straight from
// the local docker daemon (NewLocalOciPushPlan).
type OciPushPlan struct {
	push        func(context.Context, *worker.ProgressTracker) (bool, error)
	destination string
	targetLabel string
	reporter    *PushReporter
}

func NewOciPushPlan(pusher ImagePusher, image *gen.OCIImageOutput, destination, targetLabel string, reporter *PushReporter) *OciPushPlan {
	return &OciPushPlan{
		push: func(ctx context.Context, tracker *worker.ProgressTracker) (bool, error) {
			return pusher.PushImage(ctx, image, destination, tracker)
		},
		destination: destination,
		targetLabel: targetLabel,
		reporter:    reporter,
	}
}

// NewLocalOciPushPlan pushes an image that only exists in the local docker
// daemon. Targets that skip the cache never stage their oci outputs, so there
// is no cached copy for the daemon-free path to read.
func NewLocalOciPushPlan(pusher ImagePusher, localTag, destination, targetLabel string, reporter *PushReporter) *OciPushPlan {
	return &OciPushPlan{
		push: func(ctx context.Context, tracker *worker.ProgressTracker) (bool, error) {
			return pusher.PushLocalImage(ctx, localTag, destination, tracker)
		},
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

	if !p.reporter.Claim(p.targetLabel, p.destination) {
		return nil
	}

	tracker.SetStatus(fmt.Sprintf("%s: pushing %s", p.targetLabel, p.destination))

	skipped, err := p.push(ctx, tracker)
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
