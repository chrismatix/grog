package dag

import (
	"context"
	"errors"
	"grog/internal/console"
	"grog/internal/label"
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
type WalkCallback func(ctx context.Context, node model.BuildNode) (CacheResult, error)

type Completion struct {
	IsSuccess   bool
	NodeType    model.NodeType
	CacheResult CacheResult
	Err         error
}

type nodeInfo struct {
	// node routine sends this when it's done
	done chan Completion
	// node routine receives this when it's ready
	ready chan interface{}
	// node routine receives this when it is supposed to stop
	cancel chan interface{}

	cancelOnce sync.Once
}

// Walker walks the graph in topological order.
// Not thread-safe.
type Walker struct {
	graph        *DirectedTargetGraph
	walkCallback WalkCallback
	nodeInfoMap  map[label.TargetLabel]*nodeInfo
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
	// nodeMutex protects nodeInfoMap
	nodeMutex sync.Mutex
	wait      sync.WaitGroup
}

func NewWalker(graph *DirectedTargetGraph, walkFunc WalkCallback, failFast bool) *Walker {
	return &Walker{
		graph:        graph,
		walkCallback: walkFunc,
		nodeInfoMap:  map[label.TargetLabel]*nodeInfo{},
		completions:  map[label.TargetLabel]Completion{},
		failFast:     failFast,
	}
}

/*
Walk For each node generate an info payload containing
- a done channel
- a cancel channel
- a start channel

Procedure:
- Start all routines that do not have dependencies
- For each routine there is a fanout onComplete function
- When it receives a doneCh it marks the worker as completed
- The onComplete then checks for each *dependant* if all their dependencies are satisfied
- If that is the case we send a message to the dependant's readyCh to start them
- In case of failure we cancel them and in case of failFast we cancel all outstanding targets

Note: We do not start routines for targets that are not selected. Therefore,
we need to check if descendants exist before starting/cancelling them.

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
	for _, node := range w.graph.nodes {
		if !node.GetIsSelected() {
			// skip unselected targets
			continue
		}

		doneCh := make(chan Completion, 1)
		readyCh := make(chan interface{}, 1)
		cancelCh := make(chan interface{}, 1)

		w.nodeInfoMap[node.GetLabel()] = &nodeInfo{
			done:   doneCh,
			ready:  readyCh,
			cancel: cancelCh,
		}

		w.wait.Add(1)
		// start all routines
		go w.nodeRoutine(ctx, node, w.nodeInfoMap[node.GetLabel()])

		// start all routines with no dependencies immediately
		if len(w.graph.inEdges[node.GetLabel()]) == 0 {
			w.startNode(node)
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
		w.cancelAll()

		if w.failFastTriggered {
			return w.completions, nil
		} else {
			return w.completions, ctx.Err()
		}
	}
}

// cancelNode cancels a target if it is present in the graph (not idempotent!)
func (w *Walker) cancelNode(node model.BuildNode) {
	w.nodeMutex.Lock()
	defer w.nodeMutex.Unlock()

	// cancel the node routine
	if info, ok := w.nodeInfoMap[node.GetLabel()]; ok {
		info.cancelOnce.Do(func() {
			close(info.cancel)
		})
	}
}

// startNode sends a ready message to a target if it is present in the graph (not idempotent!)
func (w *Walker) startNode(node model.BuildNode) {
	w.nodeMutex.Lock()
	defer w.nodeMutex.Unlock()
	if info, ok := w.nodeInfoMap[node.GetLabel()]; ok {
		go func() {
			info.ready <- true
		}()
	}
}

// onComplete called when a node is done
// - fans out ready messages to dependants (if their deps are satisfied)
// - in case of failure, cancels all dependants (or the entire walk if failFast=true)
func (w *Walker) onComplete(node model.BuildNode, completion Completion) {
	w.doneMutex.Lock()
	defer w.doneMutex.Unlock()

	// Mark node as done
	completion.NodeType = node.GetType()
	w.completions[node.GetLabel()] = completion

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
			// Cancel *all* descendants if the node failed
			for _, dep := range w.graph.GetDescendants(node) {
				w.cancelNode(dep)
			}
		}
		return
	}

	// Iterate over all dependants and send a ready message
	// if their deps are satisfied
	for _, dependant := range w.graph.outEdges[node.GetLabel()] {

		// Check if dependant deps are satisfied
		depsDone := true
		for _, dep := range w.graph.inEdges[dependant.GetLabel()] {
			depCompletion, ok := w.completions[dep.GetLabel()]
			if !ok || !depCompletion.IsSuccess {
				depsDone = false
			}
		}

		// If yes, send ready message to dependant
		if depsDone {
			w.startNode(dependant)
		}
	}
}

func (w *Walker) cancelAll() {
	for _, node := range w.graph.nodes {
		go func(v model.BuildNode) {
			w.cancelNode(v)
		}(node)
	}
}

func (w *Walker) nodeRoutine(
	ctx context.Context,
	node model.BuildNode,
	info *nodeInfo,
) {
	// always decrement wait group
	defer w.wait.Done()

	select {
	case <-info.cancel:
		return
	case <-info.ready:
		// call the callback
		cacheResult, err := w.walkCallback(ctx, node)
		if err != nil {
			if errors.Is(err, context.Canceled) {
				// Cancelling externally or via failFast leaves target uncompleted
				return
			}
			// don't account for cache hits in errors
			w.onComplete(node, Completion{IsSuccess: false, Err: err})
		} else {
			w.onComplete(node, Completion{IsSuccess: true, Err: nil, CacheResult: cacheResult})
		}
		return
	}
}
