package handlers

import (
	"context"

	"grog/internal/worker"
)

type nopWritePlan struct{}

func (nopWritePlan) Execute(context.Context, *worker.ProgressTracker) error {
	return nil
}

func (nopWritePlan) Cleanup(context.Context) error {
	return nil
}

// NewNopWritePlan returns a plan that does not perform any persistence work.
func NewNopWritePlan() OutputWritePlan {
	return nopWritePlan{}
}

// CompositeWritePlan chains multiple plans into one. Execute runs each plan in
// order and stops at the first error (the per-plan Execute decides whether
// that's recoverable). Cleanup runs every inner plan's cleanup regardless of
// outcome, mirroring how the CacheWriter's own cleanup is invoked on both
// success and failure paths.
//
// Used by OciPushHandler to bind the cache write (inherited from the docker
// handler) and the user-facing push into a single plan, so the existing
// CacheWriter loop schedules them with no further plumbing.
type CompositeWritePlan struct {
	Plans []OutputWritePlan
}

func (c *CompositeWritePlan) Execute(ctx context.Context, tracker *worker.ProgressTracker) error {
	for _, plan := range c.Plans {
		if plan == nil {
			continue
		}
		if err := plan.Execute(ctx, tracker); err != nil {
			return err
		}
	}
	return nil
}

func (c *CompositeWritePlan) Cleanup(ctx context.Context) error {
	var firstErr error
	for _, plan := range c.Plans {
		if plan == nil {
			continue
		}
		if err := plan.Cleanup(ctx); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}
