package execution

import (
	"context"
	"fmt"
	"sync"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/fatih/color"

	"grog/internal/console"
	"grog/internal/dag"
	"grog/internal/worker"
)

// PoolCoordinator composes a task worker pool and an I/O worker pool,
// merging their UI state into a single tea UI.
type PoolCoordinator struct {
	taskPool *worker.TaskWorkerPool[dag.CacheResult]
	ioPool   *worker.TaskWorkerPool[struct{}]
	sendMsg  func(tea.Msg)
	mu       sync.Mutex

	totalBuildTasks int
}

// NewPoolCoordinator creates both pools with onStateChange pointing to the coordinator's flushState.
// Task pool uses workerIdOffset=0, I/O pool uses workerIdOffset=10000.
func NewPoolCoordinator(
	logger *console.Logger,
	numTaskWorkers int,
	numIOWorkers int,
	sendMsg func(tea.Msg),
	totalBuildTasks int,
) *PoolCoordinator {
	pc := &PoolCoordinator{
		sendMsg:         sendMsg,
		totalBuildTasks: totalBuildTasks,
	}

	taskPool := worker.NewTaskWorkerPool[dag.CacheResult](logger, numTaskWorkers, sendMsg, totalBuildTasks)
	taskPool.SetOnStateChange(pc.flushState)

	ioPool := worker.NewTaskWorkerPool[struct{}](logger, numIOWorkers, sendMsg, 0)
	ioPool.SetWorkerIdOffset(10000)
	ioPool.SetOnStateChange(pc.flushState)

	pc.taskPool = taskPool
	pc.ioPool = ioPool

	return pc
}

// flushState merges task and I/O pool states and sends a single TaskStateMsg + HeaderMsg.
func (pc *PoolCoordinator) flushState() {
	pc.mu.Lock()
	defer pc.mu.Unlock()

	taskState := pc.taskPool.GetTaskState()
	ioState := pc.ioPool.GetTaskState()

	merged := make(console.TaskStateMap, len(taskState)+len(ioState))
	for k, v := range taskState {
		merged[k] = v
	}
	for k, v := range ioState {
		merged[k] = v
	}

	pc.sendMsg(console.TaskStateMsg{State: merged})

	// Build header
	taskCompleted := pc.taskPool.GetCompletedTasks()
	total := pc.totalBuildTasks
	taskRunning := pc.taskPool.GetRunningCount()
	ioRunning := pc.ioPool.GetRunningCount()

	green := color.New(color.FgGreen).SprintFunc()
	header := green(fmt.Sprintf("[%d/%d]", taskCompleted, total))

	if ioRunning > 0 {
		header += fmt.Sprintf(" %s, %s writing cache",
			console.FCount(taskRunning, "action"),
			console.FCount(ioRunning, "upload"))
	} else {
		header += fmt.Sprintf(" %s running", console.FCount(taskRunning, "action"))
	}

	pc.sendMsg(console.HeaderMsg(header))
}

// TaskPool returns the task worker pool.
func (pc *PoolCoordinator) TaskPool() *worker.TaskWorkerPool[dag.CacheResult] {
	return pc.taskPool
}

// IOPool returns the I/O worker pool.
func (pc *PoolCoordinator) IOPool() *worker.TaskWorkerPool[struct{}] {
	return pc.ioPool
}

// StartWorkers starts both pools.
func (pc *PoolCoordinator) StartWorkers(ctx context.Context) {
	pc.taskPool.StartWorkers(ctx)
	pc.ioPool.StartWorkers(ctx)
}

// Shutdown shuts down both pools.
func (pc *PoolCoordinator) Shutdown() {
	pc.taskPool.Shutdown()
	pc.ioPool.Shutdown()
}
