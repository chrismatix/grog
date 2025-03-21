package dag

import (
	"context"
	"errors"
	"grog/internal/model"
	"sync"
)

type WalkCallback func(ctx context.Context, target model.Target) error

type Completion struct {
	isSuccess bool
	err       error
}

type vertexInfo struct {
	// vertex routine sends this when it's done
	done chan Completion
	// vertex routine receives this when it's ready
	ready chan struct{}
	// vertex routine receives this when it is supposed to stop
	cancel chan struct{}
}

// Walker walks the graph in topological order.
// It makes no attempt at being externally thread safe.
type Walker struct {
	graph         *DirectedTargetGraph
	walkCb        WalkCallback
	vertexInfoMap map[*model.Target]*vertexInfo
	// Keep track of which targets have been completed
	completions map[*model.Target]Completion

	// Options
	failFast bool

	// Concurrency
	doneMutex sync.Mutex
	wait      sync.WaitGroup
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
- If that is the case we send a message to the dependant's readyCh

The main goroutine will then wait for all the goroutines to finish.
*/
func (w *Walker) Walk(ctx context.Context) error {
	if w.walkCb == nil {
		return errors.New("walk callback is nil")
	}

	// populate info map
	for _, vertex := range w.graph.vertices {
		doneCh := make(chan Completion)
		readyCh := make(chan struct{})
		cancelCh := make(chan struct{})

		w.vertexInfoMap[vertex] = &vertexInfo{
			done:   doneCh,
			ready:  readyCh,
			cancel: cancelCh,
		}

		// start a fanout channel for each vertex
		go w.onCompletionWorker(ctx, vertex, doneCh)
	}

	// start all routines
	for _, vertex := range w.graph.vertices {
		w.wait.Add(1)
		go w.vertexRoutine(*vertex, *w.vertexInfoMap[vertex])
	}

	// start all routines with no dependencies immediately
	for _, vertex := range w.graph.vertices {
		if len(w.graph.inEdges[vertex]) == 0 {
			w.vertexInfoMap[vertex].ready <- struct{}{}
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
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (w *Walker) onCompletionWorker(ctx context.Context, target *model.Target, doneCh chan Completion) {
	select {
	case completion := <-doneCh:
		w.onComplete(target, completion)
	case <-ctx.Done():
		return
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

	if !completion.isSuccess {
		// If failFast is true, cancel the entire walk
		if w.failFast {
			w.cancelAll()
		} else {
			// Cancel *all* descendants if the target failed
			for _, dep := range w.graph.GetDescendants(target) {
				w.vertexInfoMap[dep].cancel <- struct{}{}
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
			w.vertexInfoMap[dependant].ready <- struct{}{}
		}
	}
}

func (w *Walker) cancelAll() {
	for _, vertex := range w.graph.vertices {
		go func(v *model.Target) {
			w.vertexInfoMap[v].cancel <- struct{}{}
		}(vertex)
	}
}

func (w *Walker) vertexRoutine(
	target model.Target,
	info vertexInfo,
) {
	// always decrement wait group
	defer w.wait.Done()

	ctx, cancel := context.WithCancel(context.Background())
	// ensure cancel is called to prevent context leak
	defer cancel()

	// listen to all events
	select {
	case <-info.cancel:
		// this will also cancel the walkCb
		return
	case <-info.ready:
		// call the callback
		err := w.walkCb(ctx, target)
		if err != nil {
			info.done <- Completion{isSuccess: false, err: err}
			return
		}
		info.done <- Completion{isSuccess: true, err: nil}
	}
}
