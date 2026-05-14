package execution

import (
	"context"
	"sync"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest"

	"grog/internal/config"
	"grog/internal/console"
	"grog/internal/dag"
	"grog/internal/label"
	"grog/internal/model"
	"grog/internal/worker"
)

func newTestPool(t *testing.T, numWorkers int) *worker.TaskWorkerPool[dag.CacheResult] {
	t.Helper()
	logger := console.NewFromSugared(zaptest.NewLogger(t).Sugar(), zapcore.DebugLevel)
	pool := worker.NewTaskWorkerPool[dag.CacheResult](logger, numWorkers, func(_ tea.Msg) {}, 0)
	pool.StartWorkers(t.Context())
	t.Cleanup(pool.Shutdown)
	return pool
}

func newTarget(name, group string) *model.Target {
	return &model.Target{
		Label:            label.TargetLabel{Package: "p", Name: name},
		ConcurrencyGroup: group,
	}
}

// inFlightTracker reports the peak number of simultaneously-running tasks
// it's been called on, so tests can assert scheduling bounds by watching
// the task bodies themselves.
type inFlightTracker struct {
	mu      sync.Mutex
	current int
	peak    int
}

func (t *inFlightTracker) enter() {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.current++
	if t.current > t.peak {
		t.peak = t.current
	}
}

func (t *inFlightTracker) exit() {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.current--
}

func (t *inFlightTracker) peakLoad() int {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.peak
}

func TestScheduler_GroupSerializes(t *testing.T) {
	// Group with default capacity 1 fully serializes participating targets.
	// With 5 × 50ms tasks the wall-clock must be at least ~250ms even though
	// num_workers=8 would otherwise let them all run in parallel.
	pool := newTestPool(t, 8)
	s := NewScheduler(pool)

	var tracker inFlightTracker
	body := func(_ worker.StatusFunc) (dag.CacheResult, error) {
		tracker.enter()
		time.Sleep(50 * time.Millisecond)
		tracker.exit()
		return dag.CacheHit, nil
	}

	var wg sync.WaitGroup
	wg.Add(5)
	start := time.Now()
	for i := 0; i < 5; i++ {
		tgt := newTarget("t", "serial_group")
		go func() {
			defer wg.Done()
			_, _ = s.Schedule(t.Context(), tgt, body)
		}()
	}
	wg.Wait()
	elapsed := time.Since(start)

	if tracker.peakLoad() != 1 {
		t.Fatalf("expected peak concurrency 1 within group, got %d", tracker.peakLoad())
	}
	if elapsed < 240*time.Millisecond {
		t.Fatalf("expected elapsed >= 240ms (serialized), got %v", elapsed)
	}
}

func TestScheduler_GroupBoundedCapacity(t *testing.T) {
	// Group with capacity=2 should cap concurrent members at 2 even when
	// more slots are available globally.
	config.Global.ConcurrencyGroups = map[string]int{"bounded": 2}
	t.Cleanup(func() { config.Global.ConcurrencyGroups = nil })

	pool := newTestPool(t, 8)
	s := NewScheduler(pool)

	var tracker inFlightTracker
	body := func(_ worker.StatusFunc) (dag.CacheResult, error) {
		tracker.enter()
		time.Sleep(30 * time.Millisecond)
		tracker.exit()
		return dag.CacheHit, nil
	}

	var wg sync.WaitGroup
	wg.Add(5)
	for i := 0; i < 5; i++ {
		tgt := newTarget("t", "bounded")
		go func() {
			defer wg.Done()
			_, _ = s.Schedule(t.Context(), tgt, body)
		}()
	}
	wg.Wait()

	if peak := tracker.peakLoad(); peak > 2 {
		t.Fatalf("expected peak concurrency <= 2 within bounded group, got %d", peak)
	}
}

func TestScheduler_CancellationReleasesResources(t *testing.T) {
	// Fill the group capacity, then cancel a pending acquirer. The cancelled
	// target must bail out with ctx.Err() and release nothing (it never
	// acquired). The busy target must still complete.
	config.Global.ConcurrencyGroups = map[string]int{"cancel_group": 1}
	t.Cleanup(func() { config.Global.ConcurrencyGroups = nil })

	pool := newTestPool(t, 4)
	s := NewScheduler(pool)

	hold := make(chan struct{})
	holderDone := make(chan struct{})
	go func() {
		defer close(holderDone)
		_, _ = s.Schedule(t.Context(), newTarget("holder", "cancel_group"), func(_ worker.StatusFunc) (dag.CacheResult, error) {
			<-hold
			return dag.CacheHit, nil
		})
	}()
	// Let the holder acquire.
	time.Sleep(30 * time.Millisecond)

	ctx, cancel := context.WithCancel(context.Background())
	cancelResult := make(chan error, 1)
	go func() {
		_, err := s.Schedule(ctx, newTarget("waiter", "cancel_group"), func(_ worker.StatusFunc) (dag.CacheResult, error) {
			t.Error("cancelled waiter should never run")
			return dag.CacheHit, nil
		})
		cancelResult <- err
	}()
	// Let the waiter block on group acquire.
	time.Sleep(30 * time.Millisecond)
	cancel()

	select {
	case err := <-cancelResult:
		if err == nil {
			t.Fatal("expected error from cancelled Schedule, got nil")
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("cancelled Schedule did not return in time")
	}

	close(hold)
	select {
	case <-holderDone:
	case <-time.After(500 * time.Millisecond):
		t.Fatal("holder task did not finish")
	}
}
