package worker

import (
	"sync"
	"testing"
)

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
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 4; j++ {
				child.Add(8 * 1024)
			}
		}()
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
