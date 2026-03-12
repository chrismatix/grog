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

// AsyncUploadManager collects deferred upload tasks and runs them
// in a bounded goroutine pool after builds complete.
type AsyncUploadManager struct {
	mu        sync.Mutex
	tasks     []func(ctx context.Context) error
	submitted atomic.Int64
	completed atomic.Int64
}

func NewAsyncUploadManager() *AsyncUploadManager {
	return &AsyncUploadManager{}
}

// Submit appends a deferred upload task.
func (m *AsyncUploadManager) Submit(task func(ctx context.Context) error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.tasks = append(m.tasks, task)
	m.submitted.Add(1)
}

// Submitted returns the number of submitted tasks.
func (m *AsyncUploadManager) Submitted() int64 {
	return m.submitted.Load()
}

// Wait runs all tasks in a bounded goroutine pool, sending progress updates
// via sendMsg to the tea UI. Returns collected errors (non-fatal).
func (m *AsyncUploadManager) Wait(ctx context.Context, workers int, sendMsg func(tea.Msg)) []error {
	m.mu.Lock()
	tasks := m.tasks
	m.tasks = nil
	m.mu.Unlock()

	total := int64(len(tasks))
	if total == 0 {
		return nil
	}

	var (
		errMu  sync.Mutex
		errors []error
		wg     sync.WaitGroup
	)

	green := color.New(color.FgGreen).SprintFunc()

	// Send initial header
	sendMsg(console.HeaderMsg(
		green(fmt.Sprintf("[0/%d]", total)) + " uploading to remote cache",
	))

	// Bounded worker pool via semaphore channel
	sem := make(chan struct{}, workers)

	for _, task := range tasks {
		select {
		case <-ctx.Done():
			errMu.Lock()
			errors = append(errors, ctx.Err())
			errMu.Unlock()
			break
		case sem <- struct{}{}:
		}

		wg.Add(1)
		go func(fn func(ctx context.Context) error) {
			defer wg.Done()
			defer func() { <-sem }()

			// Set task state for this upload
			completed := m.completed.Load()
			taskState := console.TaskStateMap{
				1: {
					Status:       fmt.Sprintf("uploading %d/%d", completed+1, total),
					StartedAtSec: time.Now().Unix(),
				},
			}
			sendMsg(console.TaskStateMsg{State: taskState})

			if err := fn(ctx); err != nil {
				errMu.Lock()
				errors = append(errors, err)
				errMu.Unlock()
			}

			newCompleted := m.completed.Add(1)
			sendMsg(console.HeaderMsg(
				green(fmt.Sprintf("[%d/%d]", newCompleted, total)) + " uploading to remote cache",
			))
		}(task)
	}

	wg.Wait()

	// Clear task state when done
	sendMsg(console.TaskStateMsg{State: console.TaskStateMap{}})

	return errors
}
