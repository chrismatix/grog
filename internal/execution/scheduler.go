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

// Scheduler gates target execution on two resources:
//   - a global slot pool of num_workers, held for the target's Weight
//   - an optional named concurrency group, capacity from grog.toml
//     [concurrency_groups] (default 1), also held for the target's Weight
//
// Weights are validated at load time so a target cannot request more than
// either capacity can ever provide. The group permit is acquired BEFORE the
// slot permits to avoid a wide target holding N slots while blocked on a
// contended group.
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

// Schedule runs task once the target's concurrency resources are available.
// The pool itself enforces num_workers; Schedule layers group-level
// serialization on top.
func (s *Scheduler) Schedule(
	ctx context.Context,
	target *model.Target,
	task worker.TaskFunc[dag.CacheResult],
) (dag.CacheResult, error) {
	weight := target.Weight
	if weight < 1 {
		weight = 1
	}

	if target.ConcurrencyGroup != "" {
		group := s.groupFor(target.ConcurrencyGroup)
		if err := group.Acquire(ctx, int64(weight)); err != nil {
			return dag.CacheMiss, fmt.Errorf(
				"acquiring concurrency group %q for target %s: %w",
				target.ConcurrencyGroup, target.Label, err,
			)
		}
		defer group.Release(int64(weight))
	}

	return s.pool.RunWeighted(task, weight)
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
