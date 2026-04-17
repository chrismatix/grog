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
	"golang.org/x/sync/semaphore"

	"grog/internal/config"
	"grog/internal/console"
)

type StatusFunc func(StatusUpdate)

type StatusUpdate struct {
	Status    string
	SubStatus string
	Progress  *console.Progress
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

// TaskWorkerPool runs tasks under a weighted-semaphore admission policy.
// maxWorkers is the total capacity; each task consumes one slot by default,
// or weight slots when submitted via RunWeighted. Tasks execute on the
// caller's goroutine (or a spawned goroutine for RunFireAndForget), so the
// pool bounds concurrent execution without carrying a long-lived worker
// goroutine per slot.
type TaskWorkerPool[T any] struct {
	logger     *console.Logger
	maxWorkers int
	totalTasks int

	// slots bounds concurrent task execution to maxWorkers total units of
	// weight. Acquire is weight-aware so one multi-slot task can reserve
	// several units at once.
	slots *semaphore.Weighted

	// freeIds hands out 1..maxWorkers display slot IDs used as keys in the
	// UI taskState map. Each running task occupies exactly one display slot
	// regardless of its weight; extra weight reduces how many siblings can
	// run but is not separately visualized.
	freeIds chan int

	sendMsg func(msg tea.Msg)

	taskState      console.TaskStateMap
	nextTaskId     int
	completedTasks int

	workerIdOffset int
	onStateChange  func()

	activeJobs sync.WaitGroup

	poolCtx      context.Context
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

	freeIds := make(chan int, maxWorkers)
	for i := 1; i <= maxWorkers; i++ {
		freeIds <- i
	}

	return &TaskWorkerPool[T]{
		logger:     logger,
		maxWorkers: maxWorkers,
		totalTasks: totalTasks,
		slots:      semaphore.NewWeighted(int64(maxWorkers)),
		freeIds:    freeIds,
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

// StartWorkers wires the pool to a context. No long-lived worker goroutines
// are spawned; the context is used to abort in-flight Acquire calls on
// cancellation and to trigger Shutdown.
func (twp *TaskWorkerPool[T]) StartWorkers(ctx context.Context) {
	twp.poolCtx = ctx
	twp.watcherOnce.Do(func() {
		go func() {
			<-ctx.Done()
			twp.Shutdown()
		}()
	})
}

// run is the internal execution path. Callers are responsible for
// managing activeJobs lifecycle before invoking this.
func (twp *TaskWorkerPool[T]) run(task TaskFunc[T], weight int) (T, error) {
	var zero T

	if weight < 1 {
		weight = 1
	}
	if weight > twp.maxWorkers {
		return zero, fmt.Errorf("task weight %d exceeds num_workers %d", weight, twp.maxWorkers)
	}

	ctx := twp.poolCtx
	if ctx == nil {
		ctx = context.Background()
	}

	if err := twp.slots.Acquire(ctx, int64(weight)); err != nil {
		return zero, err
	}
	defer twp.slots.Release(int64(weight))

	// Bail after acquire in case the pool was shut down while we waited.
	if twp.closed.Load() {
		return zero, fmt.Errorf("worker pool is closed")
	}

	var workerId int
	select {
	case workerId = <-twp.freeIds:
	case <-ctx.Done():
		return zero, ctx.Err()
	}
	defer func() { twp.freeIds <- workerId }()

	twp.mu.Lock()
	id := twp.nextTaskId
	twp.nextTaskId++
	twp.mu.Unlock()
	twp.logger.Debugf("Starting task %d on worker %d (weight=%d)", id+1, workerId, weight)

	isDebug := config.Global.IsDebug()
	res, err := task(func(status StatusUpdate) {
		taskStatus := status.Status
		if isDebug {
			if weight > 1 {
				taskStatus = fmt.Sprintf("%s (worker %d ×%d)", status.Status, workerId, weight)
			} else {
				taskStatus = fmt.Sprintf("%s (worker %d)", status.Status, workerId)
			}
		}
		twp.setTaskState(workerId, StatusUpdate{Status: taskStatus, SubStatus: status.SubStatus, Progress: status.Progress}, zapcore.InfoLevel)
	})
	twp.completeTask(workerId)
	return res, err
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
		twp.taskState[key] = console.TaskState{Status: status.Status, SubStatus: status.SubStatus, Progress: status.Progress, StartedAtSec: time.Now().Unix()}
	} else {
		state.Status = status.Status
		state.SubStatus = status.SubStatus
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

// Run executes task, occupying one worker slot for its lifetime.
func (twp *TaskWorkerPool[T]) Run(task TaskFunc[T]) (T, error) {
	return twp.RunWeighted(task, 1)
}

// RunWeighted executes task, occupying weight worker slots for its lifetime.
// Weight must satisfy 1 <= weight <= num_workers; out-of-range weights
// are rejected by the validator during package loading, but the pool also
// returns an error at runtime if a caller slips one through.
func (twp *TaskWorkerPool[T]) RunWeighted(task TaskFunc[T], weight int) (T, error) {
	var zero T
	if twp.closed.Load() {
		return zero, fmt.Errorf("worker pool is closed")
	}

	twp.activeJobs.Add(1)
	defer twp.activeJobs.Done()

	return twp.run(task, weight)
}

// Shutdown stops accepting new jobs. In-flight jobs that have already
// acquired slot permits finish normally.
func (twp *TaskWorkerPool[T]) Shutdown() {
	twp.shutdownOnce.Do(func() {
		twp.closed.Store(true)
	})
}

// WaitForCompletion blocks until all submitted jobs have finished.
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

// RunFireAndForget executes task on a background goroutine, returning
// immediately. The returned error signals only that submission failed
// (e.g. the pool is closed); task errors are discarded.
func (twp *TaskWorkerPool[T]) RunFireAndForget(task TaskFunc[T]) error {
	if twp.closed.Load() {
		return fmt.Errorf("worker pool is closed")
	}

	twp.activeJobs.Add(1)
	go func() {
		defer twp.activeJobs.Done()
		_, _ = twp.run(task, 1)
	}()
	return nil
}
