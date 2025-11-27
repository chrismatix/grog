package output

import (
	"context"
	"fmt"
	"runtime"
	"sync"
	"sync/atomic"
	"time"

	"grog/internal/caching"
	"grog/internal/config"
	"grog/internal/console"
	"grog/internal/hashing"
	"grog/internal/maps"
	"grog/internal/model"
	"grog/internal/output/handlers"
	"grog/internal/proto/gen"
	"grog/internal/worker"

	"github.com/alitto/pond/v2"
)

// Registry manages the available output handlers
type Registry struct {
	handlers     map[string]handlers.Handler
	cas          *caching.Cas
	pool         pond.Pool
	handlerMutex sync.RWMutex
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
	cas *caching.Cas,
) *Registry {
	r := &Registry{
		handlers:        make(map[string]handlers.Handler),
		targetMutexMap:  maps.NewMutexMap(),
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
		r.Register(handlers.NewDockerRegistryOutputHandler(cas, config.Global.Docker))
	} else {
		// The backend setting is validated in the config package
		// so we can assume it's either "docker" or "fs-tarball"
		r.Register(handlers.NewDockerOutputHandler(cas))
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
		outputType = string(handlers.FileHandler)
	case *gen.Output_Directory:
		outputType = string(handlers.DirHandler)
	case *gen.Output_DockerImage:
		outputType = string(handlers.DockerHandler)
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

func (r *Registry) WriteOutputs(ctx context.Context, target *model.Target, update worker.StatusFunc) (*gen.TargetResult, error) {
	r.targetMutexMap.Lock(target.Label.String())
	defer r.targetMutexMap.Unlock(target.Label.String())

	logger := console.GetLogger(ctx)
	logger.Debugf("%s: writing outputs", target.Label)

	outputs := target.AllOutputs()

	progress := worker.NewProgressTracker(
		fmt.Sprintf("%s: writing outputs", target.Label),
		0,
		update,
	)

	var tasks []pond.Task
	var targetOutputs []*gen.Output
	var outputsMutex sync.Mutex

	for _, outputRef := range outputs {
		localOutputRef := outputRef
		task := r.pool.SubmitErr(func() error {
			output, err := r.mustGetHandler(localOutputRef.Type).Write(ctx, *target, localOutputRef, progress)
			if err != nil {
				return err
			}
			outputsMutex.Lock()
			targetOutputs = append(targetOutputs, output)
			outputsMutex.Unlock()
			return nil
		})
		tasks = append(tasks, task)
	}

	for _, task := range tasks {
		if err := task.Wait(); err != nil {
			return nil, err
		}
	}

	outputHash, err := getOutputHash(targetOutputs)
	if err != nil {
		return nil, err
	}

	return &gen.TargetResult{
		ChangeHash:              target.ChangeHash,
		OutputHash:              outputHash,
		Outputs:                 targetOutputs,
		ExecutionDurationMillis: target.ExecutionTime.Milliseconds(),
	}, nil
}

// GetNoCacheOutputHash computes the output hash for a target when target caching is disabled
// using handler.GetHash() on local resources only
func (r *Registry) GetNoCacheOutputHash(ctx context.Context, target *model.Target) (*gen.TargetResult, error) {
	outputs := target.AllOutputs()

	var tasks []pond.Task
	var digests []string
	var outputsMutex sync.Mutex

	for _, outputRef := range outputs {
		localOutputRef := outputRef
		task := r.pool.SubmitErr(func() error {
			outputDigest, err := r.mustGetHandler(localOutputRef.Type).Hash(ctx, *target, localOutputRef)
			if err != nil {
				return err
			}
			outputsMutex.Lock()
			digests = append(digests, outputDigest)
			outputsMutex.Unlock()
			return nil
		})
		tasks = append(tasks, task)
	}

	for _, task := range tasks {
		if err := task.Wait(); err != nil {
			return nil, err
		}
	}

	return &gen.TargetResult{
		ChangeHash:              target.ChangeHash,
		OutputHash:              hashing.HashStrings(digests),
		ExecutionDurationMillis: target.ExecutionTime.Milliseconds(),
	}, nil
}

// LoadOutputs loads the outputs for a target once using the cached targetResult
func (r *Registry) LoadOutputs(
	ctx context.Context,
	target *model.Target,
	targetResult *gen.TargetResult,
	update worker.StatusFunc,
) error {
	start := time.Now()
	defer func() {
		r.addCacheDuration(time.Since(start))
	}()
	r.targetMutexMap.Lock(target.Label.String())
	defer r.targetMutexMap.Unlock(target.Label.String())
	if target.OutputsLoaded {
		// Outputs are already loaded, nothing to do
		return nil
	}

	logger := console.GetLogger(ctx)
	logger.Debugf("%s: loading outputs", target.Label)

	progress := worker.NewProgressTracker(
		fmt.Sprintf("%s: loading outputs", target.Label),
		0,
		update,
	)

	var tasks []pond.Task
	for _, outputRef := range targetResult.Outputs {
		localOutputRef := outputRef
		task := r.pool.SubmitErr(func() error {
			err := r.mustGetHandlerFromProto(localOutputRef).Load(ctx, *target, localOutputRef, progress)
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
	target.OutputHash = targetResult.OutputHash
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
