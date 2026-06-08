package worker

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest"

	"grog/internal/config"
	"grog/internal/console"
)

func newTestLogger(t *testing.T) *console.Logger {
	t.Helper()
	return console.NewFromSugared(zaptest.NewLogger(t).Sugar(), zapcore.DebugLevel)
}

func TestStatusHelpers(t *testing.T) {
	u := Status("running")
	if u.Status != "running" || u.Progress != nil {
		t.Fatalf("Status helper unexpected: %+v", u)
	}
	p := &console.Progress{Current: 1, Total: 2}
	u2 := StatusWithProgress("doing", p)
	if u2.Status != "doing" || u2.Progress != p {
		t.Fatalf("StatusWithProgress unexpected: %+v", u2)
	}
}

func TestNumWorkers(t *testing.T) {
	logger := newTestLogger(t)
	pool := NewTaskWorkerPool[int](logger, 4, func(tea.Msg) {}, 0)
	if pool.NumWorkers() != 4 {
		t.Fatalf("expected 4 workers, got %d", pool.NumWorkers())
	}
}

func TestNewTaskWorkerPoolDefaultMaxWorkers(t *testing.T) {
	logger := newTestLogger(t)
	pool := NewTaskWorkerPool[int](logger, 0, func(tea.Msg) {}, 0)
	if pool.NumWorkers() < 1 {
		t.Fatal("expected default-cpu workers, got <1")
	}
	pool2 := NewTaskWorkerPool[int](logger, -3, func(tea.Msg) {}, 0)
	if pool2.NumWorkers() < 1 {
		t.Fatal("expected default-cpu workers for negative, got <1")
	}
}

func TestSetWorkerIdOffsetAndOnStateChange(t *testing.T) {
	logger := newTestLogger(t)
	var calls int32
	pool := NewTaskWorkerPool[int](logger, 1, func(tea.Msg) {}, 0)
	pool.SetWorkerIdOffset(5000)
	pool.SetOnStateChange(func() { atomic.AddInt32(&calls, 1) })
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	pool.StartWorkers(ctx)

	res, err := pool.Run(func(update StatusFunc) (int, error) {
		update(Status("hi"))
		return 7, nil
	})
	if err != nil || res != 7 {
		t.Fatalf("run: res=%d err=%v", res, err)
	}

	if atomic.LoadInt32(&calls) == 0 {
		t.Fatal("expected onStateChange callbacks")
	}
	pool.Shutdown()
}

func TestGetTaskStateAndCompletedCount(t *testing.T) {
	prev := config.Global
	config.Global = config.WorkspaceConfig{DisableNonDeterministicLogging: true}
	t.Cleanup(func() { config.Global = prev })

	logger := newTestLogger(t)
	pool := NewTaskWorkerPool[int](logger, 1, func(tea.Msg) {}, 0)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	pool.StartWorkers(ctx)

	running := make(chan struct{})
	release := make(chan struct{})
	done := make(chan struct{})

	go func() {
		_, _ = pool.Run(func(update StatusFunc) (int, error) {
			update(Status("step1"))
			close(running)
			<-release
			return 1, nil
		})
		close(done)
	}()

	<-running
	deadline := time.Now().Add(500 * time.Millisecond)
	for time.Now().Before(deadline) {
		if pool.GetRunningCount() == 1 {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}

	state := pool.GetTaskState()
	if len(state) != 1 {
		t.Fatalf("expected 1 task state entry, got %d", len(state))
	}

	close(release)
	<-done

	deadline = time.Now().Add(500 * time.Millisecond)
	for time.Now().Before(deadline) {
		if pool.GetCompletedTasks() == 1 {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if got := pool.GetCompletedTasks(); got != 1 {
		t.Fatalf("expected 1 completed task, got %d", got)
	}
	if got := pool.GetRunningCount(); got != 0 {
		t.Fatalf("expected 0 running, got %d", got)
	}
	pool.Shutdown()
}

func TestFlushStateSendsMessages(t *testing.T) {
	logger := newTestLogger(t)
	var sent int32
	pool := NewTaskWorkerPool[int](logger, 1, func(tea.Msg) { atomic.AddInt32(&sent, 1) }, 4)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	pool.StartWorkers(ctx)

	_, err := pool.Run(func(update StatusFunc) (int, error) {
		update(Status("a"))
		return 0, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if atomic.LoadInt32(&sent) == 0 {
		t.Fatal("expected at least one message sent")
	}
	pool.Shutdown()
}

func TestRunFireAndForgetOnClosedPool(t *testing.T) {
	logger := newTestLogger(t)
	pool := NewTaskWorkerPool[int](logger, 1, func(tea.Msg) {}, 0)
	pool.Shutdown()
	if err := pool.RunFireAndForget(func(StatusFunc) (int, error) { return 0, nil }); err == nil {
		t.Fatal("expected error on closed pool")
	}
}

func TestEnqueueBackstopOnFullChannel(t *testing.T) {
	logger := newTestLogger(t)
	pool := NewTaskWorkerPool[int](logger, 1, func(tea.Msg) {}, 0)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	pool.StartWorkers(ctx)

	gate := make(chan struct{})
	var wg sync.WaitGroup
	for range 3 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _ = pool.Run(func(StatusFunc) (int, error) {
				<-gate
				return 0, nil
			})
		}()
	}
	time.Sleep(50 * time.Millisecond)
	close(gate)
	wg.Wait()
	pool.Shutdown()
}

func TestSetTaskStateLogToStdoutPath(t *testing.T) {
	prev := config.Global
	config.Global = config.WorkspaceConfig{DisableNonDeterministicLogging: false}
	t.Cleanup(func() { config.Global = prev })

	logger := newTestLogger(t)
	pool := NewTaskWorkerPool[int](logger, 1, func(tea.Msg) {}, 0)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	pool.StartWorkers(ctx)
	_, err := pool.Run(func(update StatusFunc) (int, error) {
		update(Status("phase-x"))
		return 0, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	pool.Shutdown()
}

func TestSetTaskStateUpdatesExistingEntry(t *testing.T) {
	prev := config.Global
	config.Global = config.WorkspaceConfig{DisableNonDeterministicLogging: true}
	t.Cleanup(func() { config.Global = prev })

	logger := newTestLogger(t)
	pool := NewTaskWorkerPool[int](logger, 1, func(tea.Msg) {}, 0)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	pool.StartWorkers(ctx)

	_, err := pool.Run(func(update StatusFunc) (int, error) {
		update(Status("first"))
		update(Status("second"))
		update(Status("third"))
		return 0, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	pool.Shutdown()
}

func TestFlushStateSkippedOnClosedPool(t *testing.T) {
	logger := newTestLogger(t)
	pool := NewTaskWorkerPool[int](logger, 1, func(tea.Msg) { t.Fatal("should not send after close") }, 0)
	pool.Shutdown()
	pool.flushState()
}
