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

// TaskWorkerPool runs tasks that return T with a fixed number of workers,
// reporting progress to the tea UI
type TaskWorkerPool[T any] struct {
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

func NewTaskWorkerPool[T any](maxWorkers int, msgCh chan tea.Msg, totalTasks int) *TaskWorkerPool[T] {
	if maxWorkers < 1 {
		maxWorkers = runtime.NumCPU()
	}
	return &TaskWorkerPool[T]{
		maxWorkers: maxWorkers,
		totalTasks: totalTasks,
		jobCh:      make(chan job[T]),
		msgCh:      msgCh,
		taskState:  make(console.TaskStateMap),
	}
}

func (twp *TaskWorkerPool[T]) StartWorkers(ctx context.Context) {
	for i := 0; i < twp.maxWorkers; i++ {
		workerId := i + 1
		go twp.worker(ctx, workerId)
	}
}

func (twp *TaskWorkerPool[T]) worker(ctx context.Context, workerId int) {
	isDebug := config.Global.IsDebug()

	for {
		select {
		case <-ctx.Done():
			twp.closed = true
			console.GetLogger(ctx).Debugf("Worker %d context cancelled, exiting", workerId)
			return
		case j, ok := <-twp.jobCh:
			if !ok {
				// Channel closed, exit worker
				return
			}
			twp.setTaskState(workerId, fmt.Sprintf("Starting task %d on worker %d", j.id+1, workerId))
			// Run the task, passing a callback to update progress.
			result, err := j.task(func(status string) {
				taskStatus := status
				if isDebug {
					taskStatus = fmt.Sprintf("%s (worker %d)", status, workerId)
				}
				twp.setTaskState(workerId, taskStatus)
			})

			if j.result != nil {
				j.result <- TaskResult[T]{
					Return: result,
					Error:  err,
				}
				close(j.result) // Close the channel after sending the result
			}
			twp.completeTask(workerId)
		}
	}
}

// setTaskState updates the task state from a worker
func (twp *TaskWorkerPool[T]) setTaskState(workerId int, status string) {
	twp.mu.Lock()
	defer twp.mu.Unlock()
	state, ok := twp.taskState[workerId]
	if !ok {
		twp.taskState[workerId] = console.TaskState{Status: status, StartedAtSec: time.Now().Unix()}
		twp.flushState()
		return
	}

	state.Status = status
	twp.taskState[workerId] = state
	twp.flushState()
}

// setTaskState updates the task state from a worker
func (twp *TaskWorkerPool[T]) completeTask(workerId int) {
	twp.mu.Lock()
	defer twp.mu.Unlock()
	delete(twp.taskState, workerId)
	twp.completedTasks++
	twp.flushState()
}

// flushState sends the current task state to the UI.
func (twp *TaskWorkerPool[T]) flushState() {
	if twp.closed {
		return
	}

	// create a copy of the map so we don't modify the original'
	stateCopy := make(console.TaskStateMap, len(twp.taskState))
	for key, value := range twp.taskState {
		stateCopy[key] = value
	}
	// Write the current task state
	twp.msgCh <- console.TaskStateMsg{State: stateCopy}
	totalTasks := twp.nextTaskId
	if twp.totalTasks > 0 {
		totalTasks = twp.totalTasks
	}

	actionsRunning := len(twp.taskState)
	green := color.New(color.FgGreen).SprintFunc()
	twp.msgCh <- console.HeaderMsg(green(
		fmt.Sprintf("[%d/%d]", twp.completedTasks, totalTasks)) +
		fmt.Sprintf(" %s running", console.FCount(actionsRunning, "action")))
}

func (twp *TaskWorkerPool[T]) NumWorkers() int {
	return twp.maxWorkers
}

func (twp *TaskWorkerPool[T]) Run(task TaskFunc[T]) (T, error) {
	twp.mu.Lock()
	taskId := twp.nextTaskId
	twp.nextTaskId++
	twp.mu.Unlock()

	// Create a channel to receive the result from the worker.
	resultCh := make(chan TaskResult[T], 1)

	// Enqueue the task with the result channel.
	twp.jobCh <- job[T]{id: taskId, task: task, result: resultCh}

	// Wait for the result.
	result := <-resultCh
	return result.Return, result.Error
}

func (twp *TaskWorkerPool[T]) Shutdown() {
	twp.closed = true
	close(twp.jobCh)
}
