package worker

import (
	"context"
	"fmt"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/fatih/color"
	"grog/internal/config"
	"grog/internal/console"
	"runtime"
	"sync"
	"time"
)

type StatusFunc func(status string)

type TaskResult[T any] struct {
	Return T
	Error  error
}

// TaskFunc is a task submitted to the pool. It gets an update callback.
type TaskFunc[T any] func(update StatusFunc) (T, error)

type job[T any] struct {
	id     int
	task   TaskFunc[T]
	result chan TaskResult[T]
}

// Pool runs tasks that return T with a fixed number of workers,
// reporting progress to the tea UI
type Pool[T any] struct {
	maxWorkers int
	// totalTasks total number of tasks to run (0 for unlimited)
	totalTasks int
	jobCh      chan job[T]

	// msgCh is used to send status updates to the UI.
	msgCh chan tea.Msg

	// taskState keeps track of the current task state for logging
	taskState console.TaskStateMap

	nextTaskId     int
	completedTasks int
	mu             sync.Mutex

	// closed is set to true when the pool is shut down.
	// Used to prevent sending messages on the closed msgCh
	closed bool
}

func NewPool[T any](maxWorkers int, msgCh chan tea.Msg, totalTasks int) *Pool[T] {
	if maxWorkers < 1 {
		maxWorkers = runtime.NumCPU()
	}
	return &Pool[T]{
		maxWorkers: maxWorkers,
		totalTasks: totalTasks,
		jobCh:      make(chan job[T]),
		msgCh:      msgCh,
		taskState:  make(console.TaskStateMap),
	}
}

func (wp *Pool[T]) StartWorkers(ctx context.Context) {
	for i := 0; i < wp.maxWorkers; i++ {
		workerId := i + 1
		go wp.worker(ctx, workerId)
	}
}

func (wp *Pool[T]) worker(ctx context.Context, workerId int) {
	isDebug := config.IsDebug()

	for {
		select {
		case <-ctx.Done():
			wp.closed = true
			console.GetLogger(ctx).Debugf("Worker %d context cancelled, exiting", workerId)
			return
		case j, ok := <-wp.jobCh:
			if !ok {
				// Channel closed, exit worker
				return
			}
			wp.setTaskState(workerId, fmt.Sprintf("Starting task %d on worker %d", j.id+1, workerId))
			// Run the task, passing a callback to update progress.
			result, err := j.task(func(status string) {
				taskStatus := status
				if isDebug {
					taskStatus = fmt.Sprintf("%s (worker %d)", status, workerId)
				}
				wp.setTaskState(workerId, taskStatus)
			})

			if j.result != nil {
				j.result <- TaskResult[T]{
					Return: result,
					Error:  err,
				}
				close(j.result) // Close the channel after sending the result
			}
			wp.completeTask(workerId)
		}
	}
}

// setTaskState updates the task state from a worker
func (wp *Pool[T]) setTaskState(workerId int, status string) {
	wp.mu.Lock()
	defer wp.mu.Unlock()
	state, ok := wp.taskState[workerId]
	if !ok {
		wp.taskState[workerId] = console.TaskState{Status: status, StartedAtSec: time.Now().Unix()}
		wp.flushState()
		return
	}

	state.Status = status
	wp.taskState[workerId] = state
	wp.flushState()
}

// setTaskState updates the task state from a worker
func (wp *Pool[T]) completeTask(workerId int) {
	wp.mu.Lock()
	defer wp.mu.Unlock()
	delete(wp.taskState, workerId)
	wp.completedTasks++
	wp.flushState()
}

// flushState sends the current task state to the UI.
func (wp *Pool[T]) flushState() {
	if wp.closed {
		return
	}
	green := color.New(color.FgGreen).SprintFunc()
	// Write the current task state
	wp.msgCh <- console.TaskStateMsg{State: wp.taskState}
	totalTasks := wp.nextTaskId
	if wp.totalTasks > 0 {
		totalTasks = wp.totalTasks
	}

	actionsRunning := len(wp.taskState)
	wp.msgCh <- console.HeaderMsg(green(
		fmt.Sprintf("[%d/%d]", wp.completedTasks, totalTasks)) +
		fmt.Sprintf(" %s running", console.FCount(actionsRunning, "action")))
}

func (wp *Pool[T]) NumWorkers() int {
	return wp.maxWorkers
}

func (wp *Pool[T]) Run(task TaskFunc[T]) (T, error) {
	wp.mu.Lock()
	taskId := wp.nextTaskId
	wp.nextTaskId++
	wp.mu.Unlock()

	// Create a channel to receive the result from the worker.
	resultCh := make(chan TaskResult[T], 1)

	// Enqueue the task with the result channel.
	wp.jobCh <- job[T]{id: taskId, task: task, result: resultCh}

	// Wait for the result.
	result := <-resultCh
	return result.Return, result.Error
}

func (wp *Pool[T]) Shutdown() {
	close(wp.jobCh)
}
