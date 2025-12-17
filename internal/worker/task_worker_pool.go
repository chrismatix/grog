package worker

import (
	"context"
	"fmt"
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
		}
	}
}

func (twp *TaskWorkerPool[T]) setTaskState(workerId int, status StatusUpdate, lvl zapcore.Level) {
	twp.mu.Lock()
	defer twp.mu.Unlock()

	if logToStdout() {
		twp.logger.Logf(lvl, status.Status)
		return
	}

	state, exists := twp.taskState[workerId]
	if !exists {
		twp.taskState[workerId] = console.TaskState{Status: status.Status, Progress: status.Progress, StartedAtSec: time.Now().Unix()}
	} else {
		state.Status = status.Status
		state.Progress = status.Progress
		twp.taskState[workerId] = state
	}

	twp.flushStateLocked()
}

func (twp *TaskWorkerPool[T]) completeTask(workerId int) {
	twp.mu.Lock()
	defer twp.mu.Unlock()

	delete(twp.taskState, workerId)
	twp.completedTasks++
	twp.flushStateLocked()
}

func (twp *TaskWorkerPool[T]) flushStateLocked() {
	if twp.closed.Load() {
		return
	}

	// copy
	mapCopy := make(console.TaskStateMap, len(twp.taskState))
	for k, v := range twp.taskState {
		mapCopy[k] = v
	}

	twp.sendMsg(console.TaskStateMsg{State: mapCopy})

	total := twp.nextTaskId
	if twp.totalTasks > 0 {
		total = twp.totalTasks
	}
	running := len(twp.taskState)

	green := color.New(color.FgGreen).SprintFunc()
	twp.sendMsg(console.HeaderMsg(
		green(fmt.Sprintf("[%d/%d]", twp.completedTasks, total)) +
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
	defer func() {
		if r := recover(); r != nil {
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
			return fmt.Errorf("worker pool is closed")
		}
		twp.jobCh <- job
	}

	return nil
}
