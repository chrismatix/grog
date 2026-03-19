package worker

import (
	"context"
	"fmt"
	"maps"
	"runtime"
	"sync"
	"sync/atomic"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/fatih/color"
	"go.uber.org/zap/zapcore"

	"grog/internal/config"
	"grog/internal/console"
)

type StatusFunc func(StatusUpdate)

type StatusUpdate struct {
	Status   string
	Progress *console.Progress
}

func Status(status string) StatusUpdate {
	return StatusUpdate{Status: status}
}

func StatusWithProgress(status string, progress *console.Progress) StatusUpdate {
	return StatusUpdate{Status: status, Progress: progress}
}

type TaskResult[T any] struct {
	Return T
	Error  error
}

type TaskFunc[T any] func(update StatusFunc) (T, error)

type job[T any] struct {
	id     int
	task   TaskFunc[T]
	result chan TaskResult[T]
}

type TaskWorkerPool[T any] struct {
	logger     *console.Logger
	maxWorkers int
	totalTasks int

	jobCh chan job[T]

	sendMsg func(msg tea.Msg)

	taskState      console.TaskStateMap
	nextTaskId     int
	completedTasks int

	workerIdOffset int   // offset applied to worker IDs in taskState keys
	onStateChange  func() // if set, called instead of sending messages directly

	activeJobs sync.WaitGroup

	closed       atomic.Bool
	shutdownOnce sync.Once
	watcherOnce  sync.Once
	mu           sync.Mutex
}

func NewTaskWorkerPool[T any](
	logger *console.Logger,
	maxWorkers int,
	sendMsg func(msg tea.Msg),
	totalTasks int,
) *TaskWorkerPool[T] {
	if maxWorkers < 1 {
		maxWorkers = runtime.NumCPU()
	}

	return &TaskWorkerPool[T]{
		logger:     logger,
		maxWorkers: maxWorkers,
		totalTasks: totalTasks,
		jobCh:      make(chan job[T], maxWorkers),
		sendMsg:    sendMsg,
		taskState:  make(console.TaskStateMap),
	}
}

// SetWorkerIdOffset sets an offset applied to worker IDs in taskState keys.
// Must be called before StartWorkers.
func (twp *TaskWorkerPool[T]) SetWorkerIdOffset(offset int) {
	twp.workerIdOffset = offset
}

// SetOnStateChange sets a callback to be invoked instead of sending messages directly.
// Must be called before StartWorkers.
func (twp *TaskWorkerPool[T]) SetOnStateChange(fn func()) {
	twp.onStateChange = fn
}

func (twp *TaskWorkerPool[T]) StartWorkers(ctx context.Context) {
	// start workers
	for i := 0; i < twp.maxWorkers; i++ {
		workerId := i + 1
		go twp.worker(ctx, workerId)
	}
	// schedule shutdown once
	twp.watcherOnce.Do(func() {
		go func() {
			<-ctx.Done()
			twp.Shutdown()
		}()
	})
}

func (twp *TaskWorkerPool[T]) worker(ctx context.Context, workerId int) {
	isDebug := config.Global.IsDebug()

	for {
		select {
		case <-ctx.Done():
			console.GetLogger(ctx).Debugf("Worker %d context cancelled, exiting", workerId)
			return
		case j, ok := <-twp.jobCh:
			if !ok {
				return
			}

			twp.setTaskState(workerId, Status(fmt.Sprintf("Starting task %d on worker %d", j.id+1, workerId)), zapcore.DebugLevel)
			res, err := j.task(func(status StatusUpdate) {
				taskStatus := status.Status
				if isDebug {
					taskStatus = fmt.Sprintf("%s (worker %d)", status.Status, workerId)
				}
				twp.setTaskState(workerId, StatusUpdate{Status: taskStatus, Progress: status.Progress}, zapcore.InfoLevel)
			})

			if j.result != nil {
				j.result <- TaskResult[T]{Return: res, Error: err}
				close(j.result)
			}

			twp.completeTask(workerId)
			twp.activeJobs.Done()
		}
	}
}

func (twp *TaskWorkerPool[T]) setTaskState(workerId int, status StatusUpdate, lvl zapcore.Level) {
	twp.mu.Lock()

	if logToStdout() {
		twp.logger.Logf(lvl, status.Status)
		twp.mu.Unlock()
		return
	}

	key := workerId + twp.workerIdOffset
	state, exists := twp.taskState[key]
	if !exists {
		twp.taskState[key] = console.TaskState{Status: status.Status, Progress: status.Progress, StartedAtSec: time.Now().Unix()}
	} else {
		state.Status = status.Status
		state.Progress = status.Progress
		twp.taskState[key] = state
	}

	twp.mu.Unlock()
	twp.flushState()
}

func (twp *TaskWorkerPool[T]) completeTask(workerId int) {
	twp.mu.Lock()
	delete(twp.taskState, workerId+twp.workerIdOffset)
	twp.completedTasks++
	twp.mu.Unlock()
	twp.flushState()
}

// flushState sends the current state to the UI. Must be called WITHOUT twp.mu held
// to avoid deadlock when onStateChange calls back into GetTaskState.
func (twp *TaskWorkerPool[T]) flushState() {
	if twp.closed.Load() {
		return
	}

	if twp.onStateChange != nil {
		twp.onStateChange()
		return
	}

	twp.mu.Lock()
	// copy
	mapCopy := make(console.TaskStateMap, len(twp.taskState))
	maps.Copy(mapCopy, twp.taskState)

	total := twp.nextTaskId
	if twp.totalTasks > 0 {
		total = twp.totalTasks
	}
	running := len(twp.taskState)
	completed := twp.completedTasks
	twp.mu.Unlock()

	twp.sendMsg(console.TaskStateMsg{State: mapCopy})

	green := color.New(color.FgGreen).SprintFunc()
	twp.sendMsg(console.HeaderMsg(
		green(fmt.Sprintf("[%d/%d]", completed, total)) +
			fmt.Sprintf(" %s running", console.FCount(running, "action")),
	))
}

func logToStdout() bool {
	return !console.UseTea() && !config.Global.DisableNonDeterministicLogging
}

func (twp *TaskWorkerPool[T]) NumWorkers() int {
	return twp.maxWorkers
}

func (twp *TaskWorkerPool[T]) Run(task TaskFunc[T]) (T, error) {
	var zero T
	if twp.closed.Load() {
		return zero, fmt.Errorf("worker pool is closed")
	}

	twp.mu.Lock()
	id := twp.nextTaskId
	twp.nextTaskId++
	twp.mu.Unlock()

	resultCh := make(chan TaskResult[T], 1)
	job := job[T]{id: id, task: task, result: resultCh}

	if err := twp.enqueue(job); err != nil {
		return zero, err
	}

	res := <-resultCh
	return res.Return, res.Error
}

func (twp *TaskWorkerPool[T]) Shutdown() {
	twp.shutdownOnce.Do(func() {
		twp.closed.Store(true)
		close(twp.jobCh)
	})
}

func (twp *TaskWorkerPool[T]) enqueue(job job[T]) (err error) {
	twp.activeJobs.Add(1)

	defer func() {
		if r := recover(); r != nil {
			twp.activeJobs.Done() // undo the Add if enqueue fails
			if twp.closed.Load() {
				err = fmt.Errorf("worker pool is closed")
				return
			}
			panic(r)
		}
	}()

	// enqueue or bail on context cancel
	select {
	case twp.jobCh <- job:
	case <-time.After(time.Second): // backstop so we don't hang if closed
		if twp.closed.Load() {
			twp.activeJobs.Done()
			return fmt.Errorf("worker pool is closed")
		}
		twp.jobCh <- job
	}

	return nil
}

// WaitForCompletion blocks until all enqueued jobs have finished.
func (twp *TaskWorkerPool[T]) WaitForCompletion() {
	twp.activeJobs.Wait()
}

// GetTaskState returns a copy of the current task state map.
func (twp *TaskWorkerPool[T]) GetTaskState() console.TaskStateMap {
	twp.mu.Lock()
	defer twp.mu.Unlock()
	mapCopy := make(console.TaskStateMap, len(twp.taskState))
	for k, v := range twp.taskState {
		mapCopy[k] = v
	}
	return mapCopy
}

// GetCompletedTasks returns the number of completed tasks.
func (twp *TaskWorkerPool[T]) GetCompletedTasks() int {
	twp.mu.Lock()
	defer twp.mu.Unlock()
	return twp.completedTasks
}

// GetRunningCount returns the number of currently running tasks.
func (twp *TaskWorkerPool[T]) GetRunningCount() int {
	twp.mu.Lock()
	defer twp.mu.Unlock()
	return len(twp.taskState)
}

// RunFireAndForget enqueues a task without waiting for its result.
func (twp *TaskWorkerPool[T]) RunFireAndForget(task TaskFunc[T]) error {
	if twp.closed.Load() {
		return fmt.Errorf("worker pool is closed")
	}

	twp.mu.Lock()
	id := twp.nextTaskId
	twp.nextTaskId++
	twp.mu.Unlock()

	job := job[T]{id: id, task: task, result: nil}
	return twp.enqueue(job)
}
