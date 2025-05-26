package dag

import (
	"context"
	"errors"
	"grog/internal/console"
	"grog/internal/model"
	"sync"
)

type CacheResult int

const (
	// CacheHit found the cache data and loaded it successfully
	CacheHit CacheResult = iota
	// CacheSkip cache was intentionally skipped (does not invalidate downstream targets!)
	CacheSkip
	// CacheMiss either did not find the cache data or failed to load it or there was some other error
	CacheMiss
)

// WalkCallback is called for each target and should return true if the target was cached
// depsCached is true if all dependencies were cached or if there were no dependencies
type WalkCallback func(ctx context.Context, target *model.Target, depsCached bool) (CacheResult, error)

type Completion struct {
	IsSuccess   bool
	CacheResult CacheResult
	Err         error
}

type vertexInfo struct {
	// vertex routine sends this when it's done
	done chan Completion
	// vertex routine receives this when it's ready
	ready chan bool // sends depsCached
	// vertex routine receives this when it is supposed to stop
	cancel chan struct{}

	cancelOnce sync.Once
}

// Walker walks the graph in topological order.
// Not thread-safe.
type Walker struct {
	graph         *DirectedTargetGraph
	walkCallback  WalkCallback
	vertexInfoMap map[*model.Target]*vertexInfo
	// Keep track of which targets have been completed
	completions CompletionMap

	// Options
	failFast bool

	// Set to true if failFast was triggered
	failFastTriggered bool
	allCancel         context.CancelFunc

	// Concurrency
	// doneMutex protects completions
	doneMutex sync.Mutex
	// vertexMutex protects vertexInfoMap
	vertexMutex sync.Mutex
	wait        sync.WaitGroup
}

func NewWalker(graph *DirectedTargetGraph, walkFunc WalkCallback, failFast bool) *Walker {
	return &Walker{
		graph:         graph,
		walkCallback:  walkFunc,
		vertexInfoMap: map[*model.Target]*vertexInfo{},
		completions:   map[*model.Target]Completion{},
		failFast:      failFast,
	}
}

/*
Walk For each vertex generate an info payload containing
- a done channel
- a cancel channel
- a start channel

Procedure:
- Start all routines that do not have dependencies
- For each routine there is a fanout onCompletionWorker that listens for its doneCh
- When it receives a doneCh it marks the worker as completed
- The onCompletionWorker then checks for each *dependant* if all their dependencies are satisfied
- If that is the case we send a message to the dependant's readyCh to start them
- In case of failure we cancel them and in case of failFast we cancel all outstanding targets

Note: We do not start routines for targets that are not selected. Therefore,
we need to check if descendants exist before starting/cancelling them

Walk will then wait for all the goroutines to finish.
*/
func (w *Walker) Walk(
	ctx context.Context,
) (CompletionMap, error) {
	logger := console.GetLogger(ctx)

	if w.walkCallback == nil {
		return nil, errors.New("walk callback is nil")
	}

	ctx, cancelFunc := context.WithCancel(ctx)
	w.allCancel = cancelFunc

	// populate info map
	for _, vertex := range w.graph.vertices {
		if !vertex.IsSelected {
			// skip unselected targets
			continue
		}

		doneCh := make(chan Completion, 1)
		readyCh := make(chan bool)
		cancelCh := make(chan struct{})

		w.vertexInfoMap[vertex] = &vertexInfo{
			done:   doneCh,
			ready:  readyCh,
			cancel: cancelCh,
		}

		w.wait.Add(1)
		// start all routines
		go w.vertexRoutine(ctx, vertex, w.vertexInfoMap[vertex])

		// start all routines with no dependencies immediately
		if len(w.graph.inEdges[vertex.Label]) == 0 {
			w.startTarget(vertex, true)
		}
	}

	// Wait for all goroutines to complete
	done := make(chan struct{})
	go func() {
		w.wait.Wait()
		close(done)
	}()

	select {
	case <-done:
		return w.completions, nil
	case <-ctx.Done():
		logger.Debugf(
			"context cancelled, cancelling all workers",
		)
		// We must still await all walker routines
		w.wait.Wait()
		if w.failFastTriggered {
			return w.completions, nil
		} else {
			return w.completions, ctx.Err()
		}
	}
}

// cancelTarget cancels a target if it is present in the graph (not idempotent!)
func (w *Walker) cancelTarget(target *model.Target) {
	w.vertexMutex.Lock()
	defer w.vertexMutex.Unlock()

	// cancel the vertex routine
	if info, ok := w.vertexInfoMap[target]; ok {
		info.cancelOnce.Do(func() {
			close(info.cancel)
		})
	}
}

// startTarget sends a ready message to a target if it is present in the graph (not idempotent!)
func (w *Walker) startTarget(target *model.Target, depsCached bool) {
	w.vertexMutex.Lock()
	defer w.vertexMutex.Unlock()
	if info, ok := w.vertexInfoMap[target]; ok {
		go func() {
			info.ready <- depsCached
		}()
	}
}

// onComplete called when a vertex is done
// - fans out ready messages to dependants (if their deps are satisfied)
// - in case of failure, cancels all dependants (or the entire walk if failFast=true)
func (w *Walker) onComplete(target *model.Target, completion Completion) {
	w.doneMutex.Lock()
	defer w.doneMutex.Unlock()

	// Mark target as done
	w.completions[target] = completion

	if w.failFastTriggered {
		// If failFast was triggered, we assume everything is being cancelled already
		return
	}

	if !completion.IsSuccess {
		// If failFast is true, cancel the entire walk
		if w.failFast {
			w.failFastTriggered = true
			w.allCancel()
			w.cancelAll()
		} else {
			// Cancel *all* descendants if the target failed
			for _, dep := range w.graph.GetDescendants(target) {
				w.cancelTarget(dep)
			}
		}
		return
	}

	// Iterate over all dependants and send a ready message
	// if their deps are satisfied
	for _, dependant := range w.graph.outEdges[target.Label] {

		// Check if dependant deps are satisfied
		depsDone := true
		depsCached := true
		for _, dep := range w.graph.inEdges[dependant.Label] {
			depCompletion, ok := w.completions[dep]
			if !ok || !depCompletion.IsSuccess {
				depsDone = false
			}
			if depCompletion.CacheResult == CacheMiss {
				depsCached = false
			}
		}

		// If yes, send ready message to dependant
		if depsDone {
			w.startTarget(dependant, depsCached)
		}
	}
}

func (w *Walker) cancelAll() {
	for _, vertex := range w.graph.vertices {
		go func(v *model.Target) {
			w.cancelTarget(v)
		}(vertex)
	}
}

func (w *Walker) vertexRoutine(
	ctx context.Context,
	target *model.Target,
	info *vertexInfo,
) {
	// always decrement wait group
	defer w.wait.Done()

	select {
	case <-info.cancel:
		return
	case depsCached := <-info.ready:
		// call the callback
		cacheResult, err := w.walkCallback(ctx, target, depsCached)
		if err != nil {
			if errors.Is(err, context.Canceled) {
				// Cancelling externally or via failFast leaves target uncompleted
				return
			}
			// don't account for cache hits in errors
			w.onComplete(target, Completion{IsSuccess: false, Err: err})
		} else {
			if cacheResult == CacheHit && depsCached == false {
				// This should not happen and indicates an issue with the walkCallback
				// Reason: When the deps were not cached the invalidation should
				// propagate down the dependency chain
				console.GetLogger(ctx).Warnf("unexpected cache hit for target %v when deps were not cached, forcing cache miss", target.Label)
				cacheResult = CacheMiss
			}
			w.onComplete(target, Completion{IsSuccess: true, Err: nil, CacheResult: cacheResult})
		}
		return
	}
}
