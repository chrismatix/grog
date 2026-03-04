package caching

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
)

func TestBackgroundWriteGroup_SuccessfulWrites(t *testing.T) {
	bg := NewBackgroundWriteGroup(4)
	ctx := context.Background()

	var counter atomic.Int32

	for i := 0; i < 10; i++ {
		bg.Go(ctx, "test-write", func() error {
			counter.Add(1)
			return nil
		})
	}

	errs := bg.Wait()
	if len(errs) != 0 {
		t.Errorf("expected no errors, got %d", len(errs))
	}
	if counter.Load() != 10 {
		t.Errorf("expected 10 completions, got %d", counter.Load())
	}
	if bg.Total() != 10 {
		t.Errorf("expected total 10, got %d", bg.Total())
	}
	if bg.Completed() != 10 {
		t.Errorf("expected completed 10, got %d", bg.Completed())
	}
}

func TestBackgroundWriteGroup_CollectsErrors(t *testing.T) {
	bg := NewBackgroundWriteGroup(2)
	ctx := context.Background()

	bg.Go(ctx, "ok", func() error { return nil })
	bg.Go(ctx, "fail-1", func() error { return errors.New("upload failed") })
	bg.Go(ctx, "fail-2", func() error { return errors.New("timeout") })

	errs := bg.Wait()
	if len(errs) != 2 {
		t.Errorf("expected 2 errors, got %d", len(errs))
	}
}

func TestBackgroundWriteGroup_RespectsContext(t *testing.T) {
	// When the context is already cancelled before Go is called and the
	// semaphore is full, the task should not run.
	bg := NewBackgroundWriteGroup(1)

	started := make(chan struct{})
	blocker := make(chan struct{})

	// Fill the single semaphore slot with a blocking task
	bg.Go(context.Background(), "blocker", func() error {
		close(started)
		<-blocker
		return nil
	})

	// Wait for the blocker to acquire the semaphore
	<-started

	// Create a pre-cancelled context
	cancelledCtx, cancel := context.WithCancel(context.Background())
	cancel()

	var ran atomic.Bool
	bg.Go(cancelledCtx, "cancelled", func() error {
		ran.Store(true)
		return nil
	})

	// Unblock the first task
	close(blocker)

	errs := bg.Wait()
	if ran.Load() {
		t.Error("cancelled task should not have run")
	}
	if len(errs) != 1 {
		t.Fatalf("expected 1 error (cancelled), got %d: %v", len(errs), errs)
	}
}

func TestBackgroundWriteGroup_DefaultConcurrency(t *testing.T) {
	bg := NewBackgroundWriteGroup(0)
	if cap(bg.sem) != 8 {
		t.Errorf("expected default concurrency 8, got %d", cap(bg.sem))
	}
}
