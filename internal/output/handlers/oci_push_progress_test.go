package handlers

import (
	"context"
	"sync"
	"testing"

	v1 "github.com/google/go-containerregistry/pkg/v1"

	"grog/internal/worker"
)

// recordingTracker captures every status emission so tests can assert on the
// sequence and totals the bridge produced without standing up the tea UI.
type recordingTracker struct {
	mu      sync.Mutex
	updates []worker.StatusUpdate
}

func (r *recordingTracker) record(u worker.StatusUpdate) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.updates = append(r.updates, u)
}

func (r *recordingTracker) lastTotalAndCurrent() (total, current int64) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for i := len(r.updates) - 1; i >= 0; i-- {
		if p := r.updates[i].Progress; p != nil {
			return p.Total, p.Current
		}
	}
	return 0, 0
}

func TestPushProgressBridge_AggregatesByteUpdates(t *testing.T) {
	rec := &recordingTracker{}
	parent := worker.NewProgressTracker("test", 0, rec.record)

	bridge := newPushProgressBridge(context.Background(), parent, "pushing repo:1")
	defer bridge.stop()

	// Drive a sequence: total known on first update, then bytes flow in
	// chunks, then complete. The bridge should land on (total, total) at
	// the end with monotonic increments along the way.
	updates := []v1.Update{
		{Total: 1000, Complete: 250},
		{Total: 1000, Complete: 500},
		{Total: 1000, Complete: 1000},
	}
	for _, u := range updates {
		bridge.channel() <- u
	}
	bridge.stop()

	total, current := rec.lastTotalAndCurrent()
	if total != 1000 || current != 1000 {
		t.Errorf("final progress = (%d, %d), want (1000, 1000)", current, total)
	}
}

func TestPushProgressBridge_NilParent_NoCrash(t *testing.T) {
	bridge := newPushProgressBridge(context.Background(), nil, "pushing repo:1")
	bridge.channel() <- v1.Update{Total: 100, Complete: 50}
	bridge.stop()
}

func TestPushProgressBridge_IgnoresZeroTotal(t *testing.T) {
	// go-containerregistry sometimes emits Total=0 events before the writer
	// has figured out the manifest. The bridge should keep waiting rather
	// than creating a zero-total SubTracker (which the worker package
	// rejects anyway).
	rec := &recordingTracker{}
	parent := worker.NewProgressTracker("test", 0, rec.record)

	bridge := newPushProgressBridge(context.Background(), parent, "pushing repo:1")
	bridge.channel() <- v1.Update{Total: 0, Complete: 0}
	bridge.channel() <- v1.Update{Total: 500, Complete: 100}
	bridge.channel() <- v1.Update{Total: 500, Complete: 500}
	bridge.stop()

	total, current := rec.lastTotalAndCurrent()
	if total != 500 || current != 500 {
		t.Errorf("final progress = (%d, %d), want (500, 500)", current, total)
	}
}
