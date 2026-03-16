package output

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/fatih/color"

	"grog/internal/console"
)

// AsyncUploadManager fires deferred upload tasks in background goroutines
// immediately upon submission and allows awaiting all pending tasks at the end.
type AsyncUploadManager struct {
	submitted atomic.Int64
	completed atomic.Int64

	mu      sync.Mutex
	wg      sync.WaitGroup
	errMu   sync.Mutex
	errors  []error
	sem     chan struct{}
	started bool
}

func NewAsyncUploadManager() *AsyncUploadManager {
	return &AsyncUploadManager{}
}

// Submit fires a deferred upload task in a background goroutine immediately.
// Uses a bounded semaphore to limit concurrency.
func (m *AsyncUploadManager) Submit(task func(ctx context.Context) error) {
	m.mu.Lock()
	if !m.started {
		// Lazy-init the semaphore with a reasonable concurrency limit
		m.sem = make(chan struct{}, 8)
		m.started = true
	}
	m.mu.Unlock()

	m.submitted.Add(1)
	m.wg.Add(1)
	go func() {
		defer m.wg.Done()
		m.sem <- struct{}{}
		defer func() { <-m.sem }()

		if err := task(context.Background()); err != nil {
			m.errMu.Lock()
			m.errors = append(m.errors, err)
			m.errMu.Unlock()
		}
		m.completed.Add(1)
	}()
}

// Submitted returns the number of submitted tasks.
func (m *AsyncUploadManager) Submitted() int64 {
	return m.submitted.Load()
}

// Wait blocks until all submitted tasks complete, sending progress updates
// via sendMsg to the tea UI. Returns collected errors (non-fatal).
func (m *AsyncUploadManager) Wait(ctx context.Context, workers int, sendMsg func(tea.Msg)) []error {
	total := m.submitted.Load()
	if total == 0 {
		return nil
	}

	green := color.New(color.FgGreen).SprintFunc()

	// Send initial header
	sendMsg(console.HeaderMsg(
		green(fmt.Sprintf("[0/%d]", total)) + " writing to cache",
	))

	// Poll for progress while waiting
	done := make(chan struct{})
	go func() {
		m.wg.Wait()
		close(done)
	}()

	ticker := time.NewTicker(200 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-done:
			// Final update
			completed := m.completed.Load()
			sendMsg(console.HeaderMsg(
				green(fmt.Sprintf("[%d/%d]", completed, total)) + " writing to cache",
			))
			sendMsg(console.TaskStateMsg{State: console.TaskStateMap{}})

			m.errMu.Lock()
			errs := m.errors
			m.errMu.Unlock()
			return errs
		case <-ticker.C:
			completed := m.completed.Load()
			sendMsg(console.HeaderMsg(
				green(fmt.Sprintf("[%d/%d]", completed, total)) + " writing to cache",
			))
		case <-ctx.Done():
			m.errMu.Lock()
			m.errors = append(m.errors, ctx.Err())
			m.errMu.Unlock()

			m.errMu.Lock()
			errs := m.errors
			m.errMu.Unlock()
			return errs
		}
	}
}
