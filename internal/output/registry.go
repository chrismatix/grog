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
	"grog/internal/proto/gen"
	"runtime"
	"sync"
	"sync/atomic"
	"time"

	"github.com/alitto/pond/v2"
)

// Registry manages the available output handlers
type Registry struct {
	handlers     map[string]handlers.Handler
	targetCache  *caching.TargetResultCache
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
	targetCache *caching.TargetResultCache,
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
	r.Register(handlers.NewFileOutputHandler(cas))
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
func (r *Registry) mustGetHandlerFromProto(output *gen.Output) handlers.Handler {
	var outputType string

	switch output.Kind.(type) {
	case *gen.Output_File:
		outputType = "file"
	case *gen.Output_Directory:
		outputType = "directory"
	case *gen.Output_DockerImage:
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
	var targetOutputs []*gen.Output

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

	outputHash, err := getOutputHash(targetOutputs)
	if err != nil {
		return err
	}

	targetCacheEntry := &gen.TargetResult{
		ChangeHash:              target.ChangeHash,
		OutputHash:              outputHash,
		Outputs:                 targetOutputs,
		ExecutionDurationMillis: target.ExecutionTime.Milliseconds(),
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

	targetResult, err := r.targetCache.Load(ctx, target.ChangeHash)
	if err != nil {
		return err
	}

	logger := console.GetLogger(ctx)
	logger.Debugf("%s: loading outputs", target.Label)

	var tasks []pond.Task
	for _, outputRef := range targetResult.Outputs {
		localOutputRef := outputRef
		task := r.pool.SubmitErr(func() error {
			err := r.mustGetHandlerFromProto(localOutputRef).Load(ctx, *target, localOutputRef)
			if err != nil {
				return err
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
	return nil
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
