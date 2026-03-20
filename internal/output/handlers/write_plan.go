package handlers

import (
	"context"

	"grog/internal/worker"
)

type nopWritePlan struct{}

func (nopWritePlan) Upload(context.Context, *worker.ProgressTracker) error {
	return nil
}

func (nopWritePlan) Cleanup(context.Context) error {
	return nil
}

// NewNopWritePlan returns a plan that does not perform any persistence work.
func NewNopWritePlan() OutputWritePlan {
	return nopWritePlan{}
}
