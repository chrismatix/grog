package caching

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
)

// BackgroundWriteGroup coordinates deferred cache write operations.
// It collects goroutines that upload to remote backends and waits for
// all of them to finish before the build exits. Failures are collected
// and returned by Wait rather than failing the build.
type BackgroundWriteGroup struct {
	wg  sync.WaitGroup
	sem chan struct{}

	mu        sync.Mutex
	errs      []error
	total     atomic.Int32
	completed atomic.Int32
}

// NewBackgroundWriteGroup creates a new BackgroundWriteGroup with the
// given maximum concurrency for background uploads.
func NewBackgroundWriteGroup(maxConcurrency int) *BackgroundWriteGroup {
	if maxConcurrency <= 0 {
		maxConcurrency = 8
	}
	return &BackgroundWriteGroup{
		sem: make(chan struct{}, maxConcurrency),
	}
}

// Go launches fn in a background goroutine. The semaphore bounds how
// many goroutines can run concurrently. Errors are collected and
// returned by Wait.
func (bg *BackgroundWriteGroup) Go(ctx context.Context, name string, fn func() error) {
	bg.total.Add(1)
	bg.wg.Add(1)
	go func() {
		defer bg.wg.Done()
		defer bg.completed.Add(1)

		// Fast path: bail out immediately if context is already done
		if ctx.Err() != nil {
			bg.recordError(fmt.Errorf("%s: cancelled before start: %w", name, ctx.Err()))
			return
		}

		// Acquire semaphore slot
		select {
		case bg.sem <- struct{}{}:
		case <-ctx.Done():
			bg.recordError(fmt.Errorf("%s: cancelled before start: %w", name, ctx.Err()))
			return
		}
		defer func() { <-bg.sem }()

		if err := fn(); err != nil {
			bg.recordError(fmt.Errorf("%s: %w", name, err))
		}
	}()
}

func (bg *BackgroundWriteGroup) recordError(err error) {
	bg.mu.Lock()
	defer bg.mu.Unlock()
	bg.errs = append(bg.errs, err)
}

// Total returns the number of tasks that have been queued.
func (bg *BackgroundWriteGroup) Total() int {
	return int(bg.total.Load())
}

// Completed returns the number of tasks that have finished (success or failure).
func (bg *BackgroundWriteGroup) Completed() int {
	return int(bg.completed.Load())
}

// Wait blocks until all background writes complete and returns
// the collected errors (if any).
func (bg *BackgroundWriteGroup) Wait() []error {
	bg.wg.Wait()

	bg.mu.Lock()
	defer bg.mu.Unlock()
	return bg.errs
}
