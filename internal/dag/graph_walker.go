package dag

import (
	"context"
	"errors"
	"grog/internal/console"
	"grog/internal/model"
	"sync"
)

type WalkCallback func(ctx context.Context, target model.Target) error

type Completion struct {
	IsSuccess bool
	Err       error
}

type vertexInfo struct {
	// vertex routine sends this when it's done
	done chan Completion
	// vertex routine receives this when it's ready
	ready chan struct{}
	// vertex routine receives this when it is supposed to stop
	cancel chan struct{}
}

type CompletionMap map[*model.Target]Completion

func (c CompletionMap) GetErrors() []error {
	var errorList []error
	for _, completion := range c {
		if !completion.IsSuccess {
			errorList = append(errorList, completion.Err)
		}
	}
	return errorList
}

func (c CompletionMap) GetSuccesses() []*model.Target {
	var successList []*model.Target
	for target, completion := range c {
		if completion.IsSuccess {
			successList = append(successList, target)
		}
	}
	return successList
}

// Walker walks the graph in topological order.
// It makes no attempt at being externally thread safe.
type Walker struct {
	graph         *DirectedTargetGraph
	walkCb        WalkCallback
	vertexInfoMap map[*model.Target]*vertexInfo
	// Keep track of which targets have been completed
	completions CompletionMap

	// Options
	failFast bool

	// Set to true if failFast was triggered
	failFastTriggered bool

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
		walkCb:        walkFunc,
		vertexInfoMap: map[*model.Target]*vertexInfo{},
		completions:   map[*model.Target]Completion{},
		failFast:      failFast,
	}
}

/*
Walk For each vertex generate an info payload containing
- a done channel
- a start channel

Procedure:
- Start all routines that do not have dependencies
- For each routine there is a fanout worker that listens for its doneCh
- When it receives a doneCh it marks the worker as completed
- The onCompletionWorker then checks for each *dependant* if all their dependencies are satisfied
- If that is the case we send a message to the dependant's readyCh to start them

Note: We do not start routines for targets that are not selected. Therefore,
we need to check if descendants exist before starting/cancelling them

The main goroutine will then wait for all the goroutines to finish.
*/
func (w *Walker) Walk(
	ctx context.Context,
) (error, CompletionMap) {
	logger := console.GetLogger(ctx)

	if w.walkCb == nil {
		return errors.New("walk callback is nil"), nil
	}

	// populate info map
	for _, vertex := range w.graph.vertices {
		if !vertex.IsSelected {
			// skip unselected targets
			continue
		}

		doneCh := make(chan Completion)
		readyCh := make(chan struct{})
		cancelCh := make(chan struct{})

		w.vertexInfoMap[vertex] = &vertexInfo{
			done:   doneCh,
			ready:  readyCh,
			cancel: cancelCh,
		}

		w.wait.Add(1)
		// start a fanout channel for each vertex
		go w.onCompletionWorker(ctx, vertex, doneCh, cancelCh)

		w.wait.Add(1)
		// start all routines
		go w.vertexRoutine(*vertex, w.vertexInfoMap[vertex])

		// start all routines with no dependencies immediately
		if len(w.graph.inEdges[vertex]) == 0 {
			w.startTarget(vertex)
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
		return nil, w.completions
	case <-ctx.Done():
		logger.Debugf(
			"context cancelled, cancelling all workers",
		)
		w.cancelAll()
		return ctx.Err(), w.completions
	}
}

func (w *Walker) onCompletionWorker(
	ctx context.Context,
	target *model.Target,
	doneCh chan Completion,
	cancelCh chan struct{},
) {
	// always decrement wait group
	defer w.wait.Done()
	select {
	case <-cancelCh:
		// close this worker when the vertex routine is cancelled (e.g. due to failFast)
		return
	case completion := <-doneCh:
		w.onComplete(target, completion)
		return
	case <-ctx.Done():
		return
	}
}

// cancelTarget cancels a target if it is present in the graph (not idempotent!)
func (w *Walker) cancelTarget(target *model.Target) {
	w.vertexMutex.Lock()
	defer w.vertexMutex.Unlock()
	// skip if already cancelled/completed
	if _, ok := w.completions[target]; ok {
		return
	}
	// cancel the vertex routine
	if info, ok := w.vertexInfoMap[target]; ok {
		close(info.cancel)
	}
}

// startTarget sends a ready message to a target if it is present in the graph (not idempotent!)
func (w *Walker) startTarget(target *model.Target) {
	w.vertexMutex.Lock()
	defer w.vertexMutex.Unlock()
	if info, ok := w.vertexInfoMap[target]; ok {
		info.ready <- struct{}{}
	}
}

// onComplete called when a vertex is done
// - fans out ready messages to dependants (if their deps are satisfied)
// - in case of failure, cancels all dependants (or the entire walk if failFast=true)
func (w *Walker) onComplete(target *model.Target, completion Completion) {
	// Lock the completions map
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
	for _, dependant := range w.graph.outEdges[target] {

		// Check if dependant deps are satisfied
		depsDone := true
		for _, dep := range w.graph.inEdges[dependant] {
			if _, ok := w.completions[dep]; !ok {
				depsDone = false
			}
		}

		// If yes, send ready message to dependant
		if depsDone {
			w.startTarget(dependant)
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
	target model.Target,
	info *vertexInfo,
) {
	// always decrement wait group
	defer w.wait.Done()

	ctx, cancel := context.WithCancel(context.Background())
	// ensure cancel is called to prevent context leak
	defer cancel()

	// listen to all events
	select {
	case <-info.cancel:
		return
	case <-info.ready:
		// call the callback
		err := w.walkCb(ctx, target)
		if err != nil {
			go func() {
				info.done <- Completion{IsSuccess: false, Err: err}
			}()
		} else {
			go func() {
				info.done <- Completion{IsSuccess: true, Err: nil}
			}()
		}
		return
	}
}
