package worker

import (
	"context"
	"fmt"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/fatih/color"
	"go.uber.org/zap/zapcore"
	"grog/internal/config"
	"grog/internal/console"
	"runtime"
	"sync"
	"time"
)

type LogFunc func(msg string, level zapcore.Level)
type StatusFunc func(status string)

// TaskFunc is a task submitted to the pool. It gets an update callback.
type TaskFunc func(update StatusFunc, log LogFunc) error

type job struct {
	id     int
	task   TaskFunc
	result chan error // Channel to return the result
}

// Pool runs tasks with a fixed number of workers,
// reporting progress to the tea UI
type Pool struct {
	maxWorkers int
	jobCh      chan job

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

func NewPool(maxWorkers int, msgCh chan tea.Msg) *Pool {
	if maxWorkers < 1 {
		maxWorkers = runtime.NumCPU()
	}
	wp := &Pool{
		maxWorkers: maxWorkers,
		jobCh:      make(chan job),
		msgCh:      msgCh,
		taskState:  make(console.TaskStateMap),
	}
	return wp
}

func (wp *Pool) StartWorkers(ctx context.Context) {
	for i := 0; i < wp.maxWorkers; i++ {
		workerId := i + 1
		go wp.worker(ctx, workerId)
	}
}

func (wp *Pool) worker(ctx context.Context, workerId int) {
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
			err := j.task(func(status string) {
				taskStatus := status
				if isDebug {
					taskStatus = fmt.Sprintf("%s (worker %d)", status, workerId)
				}
				wp.setTaskState(workerId, taskStatus)
			}, func(msg string, level zapcore.Level) {
				wp.msgCh <- console.LogMsg{Msg: msg, Level: level}
			})

			if j.result != nil {
				j.result <- err // Send the result to the channel
				close(j.result) // Close the channel after sending the result
			}
			wp.completeTask(workerId)
		}
	}
}

// setTaskState updates the task state from a worker
func (wp *Pool) setTaskState(workerId int, status string) {
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
func (wp *Pool) completeTask(workerId int) {
	wp.mu.Lock()
	defer wp.mu.Unlock()
	delete(wp.taskState, workerId)
	wp.completedTasks++
	wp.flushState()
}

// flushState sends the current task state to the UI.
func (wp *Pool) flushState() {
	if wp.closed {
		return
	}
	green := color.New(color.FgGreen).SprintFunc()
	// Write the current task state
	wp.msgCh <- console.TaskStateMsg{State: wp.taskState}
	totalTasks := wp.nextTaskId
	actionsRunning := len(wp.taskState)
	wp.msgCh <- console.HeaderMsg(green(
		fmt.Sprintf("[%d/%d]", wp.completedTasks, totalTasks)) +
		fmt.Sprintf(" %s running", console.FCount(actionsRunning, "action")))
}

func (wp *Pool) NumWorkers() int {
	return wp.maxWorkers
}

func (wp *Pool) Run(task TaskFunc) error {
	wp.mu.Lock()
	taskId := wp.nextTaskId
	wp.nextTaskId++
	wp.mu.Unlock()

	// Create a channel to receive the result from the worker.
	resultCh := make(chan error, 1)

	// Enqueue the task with the result channel.
	wp.jobCh <- job{id: taskId, task: task, result: resultCh}

	// Wait for the result.
	err := <-resultCh
	return err
}

func (wp *Pool) RunAll(tasks []TaskFunc) error {
	var wg sync.WaitGroup
	errCh := make(chan error, len(tasks))

	for _, task := range tasks {
		wg.Add(1)
		go func(t TaskFunc) {
			defer wg.Done()
			if err := wp.Run(t); err != nil {
				errCh <- err
			}
		}(task)
	}

	wg.Wait()
	close(errCh)

	// Return the first error if any
	for err := range errCh {
		if err != nil {
			return err
		}
	}

	return nil
}

func (wp *Pool) Shutdown() {
	close(wp.jobCh)
}
