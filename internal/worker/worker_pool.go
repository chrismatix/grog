package worker

import (
	"fmt"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/fatih/color"
	"go.uber.org/zap/zapcore"
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
	wp.startWorkers()
	return wp
}

func (wp *Pool) startWorkers() {
	for i := 0; i < wp.maxWorkers; i++ {
		workerId := i + 1
		go wp.worker(workerId)
	}
}

func (wp *Pool) worker(workerId int) {
	for j := range wp.jobCh {
		wp.setTaskState(workerId, fmt.Sprintf("Task %d: Running (worker %d)", j.id+1, workerId))
		// Run the task, passing a callback to update progress.
		err := j.task(func(status string) {
			wp.setTaskState(workerId, fmt.Sprintf("Task %d: %s (worker %d)", j.id+1, status, workerId))
		}, func(msg string, level zapcore.Level) {
			// You might want to send log messages to the UI as well.
			// wp.msgCh <- console.LogMessage{Message: msg, Level: level}
			fmt.Printf("Log: %s (Level: %v)\n", msg, level) //TEMP
		})

		if j.result != nil {
			j.result <- err // Send the result to the channel
			close(j.result) // Close the channel after sending the result
		}
		wp.completeTask(workerId)
	}
}

// setTaskState updates the task state from a worker
func (wp *Pool) setTaskState(workerId int, status string) {
	state, ok := wp.taskState[workerId]
	if !ok {
		wp.taskState[workerId] = console.TaskState{Status: status, StartedAtSec: time.Now().Second()}
		wp.flushState()
		return
	}

	state.Status = status
	wp.taskState[workerId] = state
	wp.flushState()
}

// setTaskState updates the task state from a worker
func (wp *Pool) completeTask(workerId int) {
	delete(wp.taskState, workerId)
	wp.completedTasks++
	wp.flushState()
}

// flushState sends the current task state to the UI.
func (wp *Pool) flushState() {
	green := color.New(color.FgGreen).SprintFunc()
	// Write the current task state
	wp.msgCh <- wp.taskState
	totalTasks := wp.nextTaskId - 1
	actionsRunning := len(wp.taskState)
	wp.msgCh <- console.HeaderMsg(green(fmt.Sprintf("[%d/%d] %s running", wp.completedTasks, totalTasks, console.FCount(actionsRunning, "action"))))
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

func (wp *Pool) Shutdown() {
	close(wp.jobCh)
}
