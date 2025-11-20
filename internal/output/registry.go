package output

import (
	"context"
	"fmt"
	"grog/internal/caching"
	"grog/internal/config"
	"grog/internal/console"
	"grog/internal/maps"
	"grog/internal/model"
	"grog/internal/output/handlers"
	v1 "grog/internal/proto/gen"
	"runtime"
	"sync"
	"sync/atomic"
	"time"

	"github.com/alitto/pond/v2"
)

// Registry manages the available output handlers
type Registry struct {
	handlers     map[string]handlers.Handler
	targetCache  *caching.TargetCacheNew
	cas          *caching.Cas
	pool         pond.Pool
	handlerMutex sync.RWMutex
	enableCache  bool
	// Total time spent on registry operations
	cacheDurationNs atomic.Int64

	// Features like load_outputs=minimal may load outputs concurrently
	// In this case we want to make sure that that only happens once per target
	targetMutexMap *maps.MutexMap

	hashMutex       sync.RWMutex
	hashCache       map[string]string
	outputHashMutex sync.RWMutex
	outputHashCache map[string]map[model.Output]string
}

// NewRegistry creates a new registry with default handlers
func NewRegistry(
	targetCache *caching.TargetCacheNew,
	cas *caching.Cas,
	enableCache bool,
) *Registry {
	r := &Registry{
		handlers:        make(map[string]handlers.Handler),
		targetCache:     targetCache,
		targetMutexMap:  maps.NewMutexMap(),
		enableCache:     enableCache,
		hashCache:       make(map[string]string),
		outputHashCache: make(map[string]map[model.Output]string),
		pool: pond.NewPool(
			runtime.NumCPU() * 2,
		),
	}

	// Register built-in handlers
	r.Register(handlers.NewFileOutputHandler(targetCache))
	r.Register(handlers.NewDirectoryOutputHandler(cas))

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
func (r *Registry) mustGetHandlerFromProto(output *v1.Output) handlers.Handler {
	var outputType string

	switch output.Kind.(type) {
	case *v1.Output_File:
		outputType = "file"
	case *v1.Output_Directory:
		outputType = "directory"
	case *v1.Output_DockerImage:
		outputType = "docker_image"
	default:
		panic(fmt.Errorf("unknown output kind: %T", output.Kind))
	}

	return r.mustGetHandler(outputType)
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

func (r *Registry) HasCacheHit(ctx context.Context, targetResult *v1.TargetResultCache) (bool, error) {
	if !r.enableCache {
		return false, nil
	}
	start := time.Now()
	defer r.addCacheDuration(time.Since(start))
	r.targetMutexMap.Lock(targetResult.ChangeHash)
	defer r.targetMutexMap.Unlock(targetResult.ChangeHash)

	foundMiss := atomic.Bool{}
	var tasks []pond.Task

	outputs := targetResult.GetOutputs()

	for _, outputRef := range outputs {
		localOutputRef := outputRef
		task := r.pool.SubmitErr(func() error {
			handler := r.mustGetHandler(localOutputRef)
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

// WriteOutputs writes the outputs for a target once
// and then writes the target cache record to indicate that the output is cached
func (r *Registry) WriteOutputs(ctx context.Context, target *model.Target) error {
	if !r.enableCache {
		return nil
	}
	start := time.Now()
	defer r.addCacheDuration(time.Since(start))

	r.targetMutexMap.Lock(target.Label.String())
	defer r.targetMutexMap.Unlock(target.Label.String())

	if target.SkipsCache() {
		return nil
	}
	logger := console.GetLogger(ctx)
	logger.Debugf("%s: writing outputs", target.Label)

	outputs := target.AllOutputs()

	var tasks []pond.Task
	var targetOutputs []*v1.Output

	for _, outputRef := range outputs {
		localOutputRef := outputRef
		task := r.pool.SubmitErr(func() error {
			output, err := r.mustGetHandler(localOutputRef.Type).Write(ctx, *target, localOutputRef)
			if err != nil {
				return err
			}
			targetOutputs = append(targetOutputs, output)
			return nil
		})
		tasks = append(tasks, task)
	}

	for _, task := range tasks {
		if err := task.Wait(); err != nil {
			return err
		}
	}

	targetCacheEntry := &v1.TargetResult{
		ChangeHash: target.ChangeHash,
		Outputs:    targetOutputs,
	}

	target.OutputsLoaded = true

	return r.targetCache.Write(ctx, targetCacheEntry)
}

// LoadOutputs loads the outputs for a target once using target.OutputsLoaded
func (r *Registry) LoadOutputs(ctx context.Context, target *model.Target) error {
	if !r.enableCache {
		return nil
	}
	start := time.Now()
	defer r.addCacheDuration(time.Since(start))
	r.targetMutexMap.Lock(target.Label.String())
	defer r.targetMutexMap.Unlock(target.Label.String())
	if target.OutputsLoaded {
		// Outputs are already loaded, nothing to do
		return nil
	}

	logger := console.GetLogger(ctx)
	logger.Debugf("%s: loading outputs", target.Label)

	outputs := target.AllOutputs()

	var tasks []pond.Task
	for _, outputRef := range outputs {
		localOutputRef := outputRef
		task := r.pool.SubmitErr(func() error {
			hash, err := r.mustGetHandler(localOutputRef.Type).Load(ctx, *target, localOutputRef)
			if err != nil {
				return err
			}
			if hash != "" {
				r.cacheOutputHash(target, localOutputRef, hash)
			}
			return nil
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

func (r *Registry) addCacheDuration(duration time.Duration) {
	if duration <= 0 {
		return
	}
	r.cacheDurationNs.Add(duration.Nanoseconds())
}

// CacheDuration returns the total time spent on registry operations.
func (r *Registry) CacheDuration() time.Duration {
	return time.Duration(r.cacheDurationNs.Load())
}
