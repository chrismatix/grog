package handlers

import (
	"context"
	"sync"

	v1 "github.com/google/go-containerregistry/pkg/v1"

	"grog/internal/worker"
)

// pushProgressBridge translates go-containerregistry's v1.Update stream into
// our ProgressTracker model. go-containerregistry reports a single
// (Total, Complete) pair across the whole copy — there is no per-blob
// breakdown — so we surface one sub-tracker with the total byte count once
// Total is known and call Add(delta) as Complete advances.
//
// The returned channel is what callers pass to oci_push.Options.Progress.
// stop() drains and closes the bridge — call it before returning from the
// surrounding Execute so the goroutine never outlives the plan.
type pushProgressBridge struct {
	updates  chan v1.Update
	done     chan struct{}
	wg       sync.WaitGroup
	stopOnce sync.Once
}

// newPushProgressBridge spawns the drain goroutine. parent is the
// ProgressTracker the push plan was handed by the CacheWriter; nil disables
// the bridge entirely.
func newPushProgressBridge(ctx context.Context, parent *worker.ProgressTracker, status string) *pushProgressBridge {
	b := &pushProgressBridge{
		updates: make(chan v1.Update, 16),
		done:    make(chan struct{}),
	}
	b.wg.Add(1)
	go b.drain(ctx, parent, status)
	return b
}

func (b *pushProgressBridge) channel() chan<- v1.Update {
	if b == nil {
		return nil
	}
	return b.updates
}

// stop signals the drain goroutine to exit and waits for it. Idempotent —
// the bridge survives Execute's defer plus tests that call stop directly.
func (b *pushProgressBridge) stop() {
	if b == nil {
		return
	}
	b.stopOnce.Do(func() {
		close(b.done)
		b.wg.Wait()
	})
}

func (b *pushProgressBridge) drain(ctx context.Context, parent *worker.ProgressTracker, status string) {
	defer b.wg.Done()

	var sub *worker.ProgressTracker
	var lastComplete int64

	process := func(update v1.Update) {
		if update.Error != nil {
			// The Copy call surfaces the error via its own return value;
			// the bridge just stops emitting progress and lets the parent
			// plan handle it.
			return
		}
		if parent == nil {
			return
		}
		if sub == nil {
			if update.Total <= 0 {
				return
			}
			sub = parent.SubTracker(status, update.Total)
			if sub == nil {
				return
			}
		}
		delta := update.Complete - lastComplete
		if delta < 0 {
			// Defensive: if the writer resets Complete (rare retry path
			// inside go-containerregistry), treat the new value as
			// absolute rather than going backwards.
			delta = update.Complete
		}
		if delta > 0 {
			sub.Add(delta)
			lastComplete = update.Complete
		}
		if update.Total > 0 && update.Complete >= update.Total {
			sub.Complete()
			sub = nil
			lastComplete = 0
		}
	}

	for {
		select {
		case <-ctx.Done():
			return
		case update := <-b.updates:
			process(update)
		case <-b.done:
			// stop() was called. Drain any updates that landed in the
			// channel buffer before exiting so the parent tracker reflects
			// the final state of the push.
			for {
				select {
				case update := <-b.updates:
					process(update)
				default:
					return
				}
			}
		}
	}
}
