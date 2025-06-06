package output

import (
	"context"
	"fmt"
	"github.com/alitto/pond/v2"
	"grog/internal/caching"
	"grog/internal/config"
	"grog/internal/maps"
	"grog/internal/model"
	"grog/internal/output/handlers"
	"runtime"
	"sync"
	"sync/atomic"
)

// Registry manages the available output handlers
type Registry struct {
	handlers     map[string]handlers.Handler
	targetCache  *caching.TargetCache
	pool         pond.Pool
	handlerMutex sync.RWMutex
	enableCache  bool

	// Features like load_outputs=minimal may load outputs concurrently
	// In this case we want to make sure that that only happens once per target
	targetMutexMap *maps.MutexMap
}

// NewRegistry creates a new registry with default handlers
func NewRegistry(
	targetCache *caching.TargetCache,
	enableCache bool,
) *Registry {
	r := &Registry{
		handlers:       make(map[string]handlers.Handler),
		targetCache:    targetCache,
		targetMutexMap: maps.NewMutexMap(),
		enableCache:    enableCache,
		pool: pond.NewPool(
			runtime.NumCPU() * 2,
		),
	}

	// Register built-in handlers
	r.Register(handlers.NewFileOutputHandler(targetCache))
	r.Register(handlers.NewDirectoryOutputHandler(targetCache))

	dockerBackend := config.Global.Docker.Backend
	if dockerBackend == "registry" {
		r.Register(handlers.NewDockerRegistryOutputHandler(targetCache, config.Global.Docker))
	} else {
		// The backend setting is validated in the config package
		// so we can assume it's either "docker" or "fs-tarball"
		r.Register(handlers.NewDockerOutputHandler(targetCache))
	}
	return r
}

// Register adds a new output handler to the registry
func (r *Registry) Register(handler handlers.Handler) {
	r.handlerMutex.Lock()
	defer r.handlerMutex.Unlock()

	if _, ok := r.handlers[string(handler.Type())]; ok {
		panic(fmt.Sprintf("handler for type %s already registered", handler.Type()))
	}
	r.handlers[string(handler.Type())] = handler
}

// GetHandler retrieves a handler by type
func (r *Registry) mustGetHandler(outputType string) handlers.Handler {
	r.handlerMutex.RLock()
	defer r.handlerMutex.RUnlock()
	handler, exists := r.handlers[outputType]
	if !exists {
		panic(fmt.Sprintf("handler for type %s not registered", outputType))
	}
	return handler
}

func (r *Registry) HasCacheHit(ctx context.Context, target *model.Target) (bool, error) {
	if !r.enableCache {
		return false, nil
	}
	r.targetMutexMap.Lock(target.Label.String())
	defer r.targetMutexMap.Unlock(target.Label.String())
	// check for the default file system key for checking if the inputs changed
	cacheHit, err := r.targetCache.HasCacheExistsFile(ctx, *target)
	if err != nil {
		return false, err
	}
	// short-circuit
	if !cacheHit {
		return false, nil
	}

	if target.SkipsCache() {
		// Ignore the outputs but still use the cache exists file to check if inputs changed
		return cacheHit, nil
	}

	foundMiss := atomic.Bool{}
	var tasks []pond.Task
	for _, outputRef := range target.AllOutputs() {
		localOutputRef := outputRef
		task := r.pool.SubmitErr(func() error {
			handler := r.mustGetHandler(localOutputRef.Type)
			handlerCacheHit, handlerErr := handler.Has(ctx, *target, localOutputRef)
			if handlerErr != nil {
				return fmt.Errorf("error checking for output %s with %s handler: %w", localOutputRef, handler.Type(), handlerErr)
			}
			if !handlerCacheHit {
				foundMiss.Store(true)
			}
			return nil
		})
		tasks = append(tasks, task)
	}

	for _, t := range tasks {
		if err := t.Wait(); err != nil {
			return false, err
		}
	}

	return !foundMiss.Load(), nil
}

func (r *Registry) WriteOutputs(ctx context.Context, target *model.Target) error {
	if !r.enableCache {
		return nil
	}

	r.targetMutexMap.Lock(target.Label.String())
	defer r.targetMutexMap.Unlock(target.Label.String())
	if err := r.targetCache.WriteCacheExistsFile(ctx, *target); err != nil {
		return err
	}

	if target.SkipsCache() {
		return nil
	}

	outputs := target.AllOutputs()

	var tasks []pond.Task
	for _, outputRef := range outputs {
		localOutputRef := outputRef
		task := r.pool.SubmitErr(func() error {
			return r.mustGetHandler(localOutputRef.Type).Write(ctx, *target, localOutputRef)
		})
		tasks = append(tasks, task)
	}

	for _, task := range tasks {
		if err := task.Wait(); err != nil {
			return err
		}
	}
	return nil
}

// LoadOutputs loads the outputs for a target once using target.OutputsLoaded
func (r *Registry) LoadOutputs(ctx context.Context, target *model.Target) error {
	if !r.enableCache {
		return nil
	}
	r.targetMutexMap.Lock(target.Label.String())
	defer r.targetMutexMap.Unlock(target.Label.String())

	if target.OutputsLoaded {
		// Outputs are already loaded, nothing to do
		return nil
	}

	outputs := target.AllOutputs()

	var tasks []pond.Task
	for _, outputRef := range outputs {
		localOutputRef := outputRef
		task := r.pool.SubmitErr(func() error {
			return r.mustGetHandler(localOutputRef.Type).Load(ctx, *target, localOutputRef)
		})
		tasks = append(tasks, task)
	}

	for _, task := range tasks {
		if err := task.Wait(); err != nil {
			return err
		}
	}

	target.OutputsLoaded = true
	// Why this is needed:
	// - When restoring from the remote cache we copy every file to the local cache
	// - However, we don't explicitly restore the cache exists file
	// TODO here we should only write the local cache exists file but it feels like the wrong place to do this
	// since the output registry should not have to know about the file cache
	return r.targetCache.WriteLocalCacheExistsFile(ctx, *target)
}
