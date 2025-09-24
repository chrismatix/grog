// workerpool_test.go
package worker

import (
	"context"
	"errors"
	tea "github.com/charmbracelet/bubbletea"
	"go.uber.org/zap/zaptest"
	"sync/atomic"
	"testing"
	"time"
)

func TestSimpleTasks(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	testLogger := zaptest.NewLogger(t).Sugar()
	pool := NewTaskWorkerPool[int](testLogger, 2, func(_ tea.Msg) {}, 0)
	pool.StartWorkers(ctx)

	total := 10
	sum := int32(0)
	for i := 0; i < total; i++ {
		i := i
		go func() {
			res, err := pool.Run(func(update StatusFunc) (int, error) {
				update("running")
				time.Sleep(10 * time.Millisecond)
				return i, nil
			})
			if err != nil {
				t.Errorf("unexpected error: %v", err)
			}
			atomic.AddInt32(&sum, int32(res))
		}()
	}

	time.Sleep(200 * time.Millisecond)
	if int(sum) != (total*(total-1))/2 {
		t.Errorf("sum mismatch: got %d", sum)
	}
}

func TestCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	testLogger := zaptest.NewLogger(t).Sugar()
	p := NewTaskWorkerPool[any](testLogger, 1, func(_ tea.Msg) {}, 0)
	p.StartWorkers(ctx)

	done := make(chan struct{})
	go func() {
		p.Run(func(update StatusFunc) (any, error) {
			time.Sleep(200 * time.Millisecond)
			return nil, nil
		})
		close(done)
	}()

	time.Sleep(10 * time.Millisecond)
	cancel()
	select {
	case <-done:
	case <-time.After(300 * time.Millisecond):
		t.Fatal("Run did not return after cancellation")
	}
}

func TestShutdownBeforeRun(t *testing.T) {
	testLogger := zaptest.NewLogger(t).Sugar()
	pool := NewTaskWorkerPool[any](testLogger, 1, func(_ tea.Msg) {}, 0)
	pool.Shutdown()

	_, err := pool.Run(func(update StatusFunc) (any, error) {
		return nil, nil
	})
	if err == nil {
		t.Fatal("expected error when running on closed pool")
	}
}

func TestPanicOnJobChannel(t *testing.T) {
	// rapidly start, shutdown, cancel, then Run
	for i := 0; i < 5; i++ {
		ctx, cancel := context.WithCancel(context.Background())
		testLogger := zaptest.NewLogger(t).Sugar()
		p := NewTaskWorkerPool[int](testLogger, 2, func(_ tea.Msg) {}, 0)
		p.StartWorkers(ctx)
		p.Shutdown() // must really close jobCh now
		cancel()     // also cancel workers
		start := time.Now()
		_, err := p.Run(func(_ StatusFunc) (int, error) { return 42, nil })
		dur := time.Since(start)
		if err == nil {
			t.Error("expected error, got nil")
		}
		if dur > 200*time.Millisecond {
			t.Errorf("Run hung for %v; expected quick error", dur)
		}
	}
}

func TestRunWithConcurrentShutdown(t *testing.T) {
	for i := 0; i < 200; i++ {
		ctx, cancel := context.WithCancel(context.Background())
		testLogger := zaptest.NewLogger(t).Sugar()
		p := NewTaskWorkerPool[int](testLogger, 1, func(_ tea.Msg) {}, 0)
		p.StartWorkers(ctx)

		done := make(chan struct{})
		go func() {
			defer close(done)
			_, _ = p.Run(func(update StatusFunc) (int, error) {
				return 42, nil
			})
		}()

		// Allow Run to start and then race Shutdown/Cancel against the send.
		time.Sleep(time.Millisecond)
		p.Shutdown()
		cancel()

		select {
		case <-done:
		case <-time.After(200 * time.Millisecond):
			t.Fatal("Run did not return in time")
		}
	}
}

func TestTaskError(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	testLogger := zaptest.NewLogger(t).Sugar()
	pool := NewTaskWorkerPool[string](testLogger, 1, func(_ tea.Msg) {}, 0)
	pool.StartWorkers(ctx)

	_, err := pool.Run(func(update StatusFunc) (string, error) {
		return "", errors.New("boom")
	})
	if err == nil || err.Error() != "boom" {
		t.Fatalf("expected boom error, got %v", err)
	}
}
