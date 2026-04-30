package execution

import (
	"context"
	"fmt"
	"sync"

	"golang.org/x/sync/semaphore"

	"grog/internal/config"
	"grog/internal/dag"
	"grog/internal/model"
	"grog/internal/worker"
)

// Scheduler gates target execution on optional named concurrency groups.
// Targets sharing a group compete for that group's capacity (default 1,
// fully serialized; tunable via grog.toml [concurrency_groups]). The
// global num_workers cap is enforced by the underlying TaskWorkerPool.
type Scheduler struct {
	pool     *worker.TaskWorkerPool[dag.CacheResult]
	groupsMu sync.Mutex
	groups   map[string]*semaphore.Weighted
}

func NewScheduler(pool *worker.TaskWorkerPool[dag.CacheResult]) *Scheduler {
	return &Scheduler{
		pool:   pool,
		groups: make(map[string]*semaphore.Weighted),
	}
}

// Schedule runs task once the target's concurrency group permit is held.
// Acquiring the group permit before submitting to the pool prevents a
// queued group-bound task from occupying a worker slot while waiting on
// a contended group.
func (s *Scheduler) Schedule(
	ctx context.Context,
	target *model.Target,
	task worker.TaskFunc[dag.CacheResult],
) (dag.CacheResult, error) {
	if target.ConcurrencyGroup != "" {
		group := s.groupFor(target.ConcurrencyGroup)
		if err := group.Acquire(ctx, 1); err != nil {
			return dag.CacheMiss, fmt.Errorf(
				"acquiring concurrency group %q for target %s: %w",
				target.ConcurrencyGroup, target.Label, err,
			)
		}
		defer group.Release(1)
	}

	return s.pool.Run(task)
}

func (s *Scheduler) groupFor(name string) *semaphore.Weighted {
	s.groupsMu.Lock()
	defer s.groupsMu.Unlock()

	if sem, ok := s.groups[name]; ok {
		return sem
	}
	capacity := config.Global.ConcurrencyGroups[name]
	if capacity < 1 {
		capacity = 1
	}
	sem := semaphore.NewWeighted(int64(capacity))
	s.groups[name] = sem
	return sem
}
