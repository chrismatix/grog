package worker

import (
	"sync"
	"testing"

	"grog/internal/config"
)

func TestProgressTrackerSetStatusEmitsUpdate(t *testing.T) {
	t.Helper()

	var mu sync.Mutex
	var updates []StatusUpdate
	tracker := NewProgressTracker("initial", 0, func(update StatusUpdate) {
		mu.Lock()
		defer mu.Unlock()
		updates = append(updates, update)
	})
	if tracker == nil {
		t.Fatalf("expected tracker")
	}

	// Setting the same status must be a no-op so callers can safely call
	// SetStatus on every daemon event without flooding the UI.
	tracker.SetStatus("initial")
	mu.Lock()
	if got := len(updates); got != 0 {
		mu.Unlock()
		t.Fatalf("expected 0 updates for unchanged status, got %d", got)
	}
	mu.Unlock()

	// A new status triggers a send even without any progress advancement.
	tracker.SetStatus("phase: preparing")
	mu.Lock()
	defer mu.Unlock()
	if len(updates) != 1 {
		t.Fatalf("expected one update, got %d", len(updates))
	}
	if got := updates[0].Status; got != "phase: preparing" {
		t.Fatalf("unexpected status: %q", got)
	}
	// No progress was recorded, so the emitted Progress payload should have a
	// zero Total — that's how the UI knows not to render a progress bar.
	if updates[0].Progress == nil || updates[0].Progress.Total != 0 {
		t.Fatalf("expected zero-total progress payload, got %+v", updates[0].Progress)
	}
}

func TestProgressTrackerAggregatesChildren(t *testing.T) {
	t.Helper()

	var mu sync.Mutex
	var updates []StatusUpdate
	tracker := NewProgressTracker("root", 0, func(update StatusUpdate) {
		mu.Lock()
		defer mu.Unlock()
		updates = append(updates, update)
	})

	if tracker == nil {
		t.Fatalf("expected tracker")
	}

	childA := tracker.SubTracker("child-a", 64*1024)
	childB := tracker.SubTracker("child-b", 64*1024)

	childA.Add(32 * 1024)
	childB.Add(32 * 1024)
	childA.Complete()
	childB.Complete()

	mu.Lock()
	defer mu.Unlock()

	if len(updates) == 0 {
		t.Fatalf("expected at least one progress update")
	}

	last := updates[len(updates)-1]
	if last.Progress == nil {
		t.Fatalf("expected progress payload")
	}

	if last.Progress.Current != 128*1024 {
		t.Fatalf("unexpected progress current: %d", last.Progress.Current)
	}

	if last.Progress.Total != 128*1024 {
		t.Fatalf("unexpected progress total: %d", last.Progress.Total)
	}
}

func TestProgressTrackerConcurrentChildren(t *testing.T) {
	t.Helper()

	var mu sync.Mutex
	var updates []StatusUpdate
	tracker := NewProgressTracker("root", 0, func(update StatusUpdate) {
		mu.Lock()
		defer mu.Unlock()
		updates = append(updates, update)
	})

	child := tracker.SubTracker("child", 256*1024)
	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() {
			for range 4 {
				child.Add(8 * 1024)
			}
		})
	}

	wg.Wait()
	child.Complete()

	mu.Lock()
	defer mu.Unlock()

	if len(updates) == 0 {
		t.Fatalf("expected updates from concurrent children")
	}

	last := updates[len(updates)-1]
	if last.Progress == nil {
		t.Fatalf("expected progress payload")
	}

	if last.Progress.Current != 256*1024 {
		t.Fatalf("unexpected progress current: %d", last.Progress.Current)
	}

	if last.Progress.Total != 256*1024 {
		t.Fatalf("unexpected progress total: %d", last.Progress.Total)
	}
}

func TestProgressTrackerPrefersParentStatusWithMultipleChildren(t *testing.T) {
	t.Helper()

	var mu sync.Mutex
	var updates []StatusUpdate
	tracker := NewProgressTracker("root", 0, func(update StatusUpdate) {
		mu.Lock()
		defer mu.Unlock()
		updates = append(updates, update)
	})

	childA := tracker.SubTracker("child-a", 1024)
	childB := tracker.SubTracker("child-b", 1024)

	childA.Add(1024)
	childB.Add(1024)

	mu.Lock()
	defer mu.Unlock()

	if len(updates) == 0 {
		t.Fatalf("expected updates")
	}

	last := updates[len(updates)-1]
	if last.Status != "root" {
		t.Fatalf("expected parent status when multiple children, got %q", last.Status)
	}
}

func TestProgressTrackerUsesChildStatusWhenOnlyChild(t *testing.T) {
	t.Helper()

	var mu sync.Mutex
	var updates []StatusUpdate
	tracker := NewProgressTracker("root", 0, func(update StatusUpdate) {
		mu.Lock()
		defer mu.Unlock()
		updates = append(updates, update)
	})

	child := tracker.SubTracker("child", 1024)
	child.Add(1024)

	mu.Lock()
	defer mu.Unlock()

	if len(updates) == 0 {
		t.Fatalf("expected updates")
	}

	last := updates[len(updates)-1]
	if last.Status != "child" {
		t.Fatalf("expected child status when only one child, got %q", last.Status)
	}

	if last.Progress == nil || last.Progress.StartedAtSec != tracker.startedAtSec {
		t.Fatalf("expected child progress to inherit start time")
	}
}

func TestProgressTrackerDisabledByConfig(t *testing.T) {
	t.Helper()

	config.Global.DisableProgressTracker = true
	t.Cleanup(func() {
		config.Global.DisableProgressTracker = false
	})

	tracker := NewProgressTracker("root", 0, func(StatusUpdate) {})
	if tracker != nil {
		t.Fatalf("expected tracker to be nil when disabled")
	}
}
