package output

import (
	"context"
	"fmt"
	"grog/internal/caching"
	"grog/internal/config"
	"grog/internal/console"
	"grog/internal/hashing"
	"grog/internal/maps"
	"grog/internal/model"
	"grog/internal/output/handlers"
	"grog/internal/proto/gen"
	"grog/internal/worker"
	"runtime"
	"slices"
	"sync"

	"github.com/alitto/pond/v2"
)

// Registry manages the available output handlers
type Registry struct {
	handlers     map[string]handlers.Handler
	cas          *caching.Cas
	pool         pond.Pool
	handlerMutex sync.RWMutex

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
	ctx context.Context,
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
		r.Register(handlers.NewDockerOutputHandler(ctx, cas))
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

func (r *Registry) WriteOutputs(
	ctx context.Context,
	target *model.Target,
	progress *worker.ProgressTracker,
) (*gen.TargetResult, error) {
	r.targetMutexMap.Lock(target.Label.String())
	defer r.targetMutexMap.Unlock(target.Label.String())

	logger := console.GetLogger(ctx)
	outputs := target.AllOutputs()

	logger.Debugf("%s: writing outputs", target.Label)

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
			logger.Debugf("%s: output %s written", target.Label, localOutputRef.Type)
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

	logger.Debugf("%s: outputs written", target.Label)

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
	progress *worker.ProgressTracker,
) error {
	r.targetMutexMap.Lock(target.Label.String())
	defer r.targetMutexMap.Unlock(target.Label.String())
	if target.OutputsLoaded {
		// Outputs are already loaded, nothing to do
		return nil
	}

	if err := validateTargetResultOutputs(target, targetResult); err != nil {
		return err
	}

	logger := console.GetLogger(ctx)
	logger.Debugf("%s: loading outputs", target.Label)

	var tasks []pond.Task
	for _, outputRef := range targetResult.Outputs {
		localOutputRef := outputRef
		logger.Debugf("%s: loading output %s", target.Label, localOutputRef.GetKind())
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

	logger.Debugf("%s: outputs loaded", target.Label)
	target.OutputsLoaded = true
	target.OutputHash = targetResult.OutputHash
	return nil
}

func validateTargetResultOutputs(target *model.Target, targetResult *gen.TargetResult) error {
	if targetResult == nil {
		return fmt.Errorf("%s: cached target result is nil", target.Label)
	}

	expectedOutputDefinitions := target.OutputDefinitions()
	loadedOutputDefinitions, err := getOutputDefinitionsFromProto(targetResult.Outputs)
	if err != nil {
		return fmt.Errorf("%s: invalid output entry in cached target result: %w", target.Label, err)
	}

	if len(expectedOutputDefinitions) != len(loadedOutputDefinitions) {
		return fmt.Errorf(
			"%s: cached outputs mismatch: expected %d outputs %v, got %d outputs %v",
			target.Label,
			len(expectedOutputDefinitions),
			expectedOutputDefinitions,
			len(loadedOutputDefinitions),
			loadedOutputDefinitions,
		)
	}

	sortedExpectedOutputDefinitions := slices.Clone(expectedOutputDefinitions)
	sortedLoadedOutputDefinitions := slices.Clone(loadedOutputDefinitions)
	slices.Sort(sortedExpectedOutputDefinitions)
	slices.Sort(sortedLoadedOutputDefinitions)

	for index := range sortedExpectedOutputDefinitions {
		if sortedExpectedOutputDefinitions[index] != sortedLoadedOutputDefinitions[index] {
			return fmt.Errorf(
				"%s: cached outputs mismatch: expected outputs %v, got outputs %v",
				target.Label,
				expectedOutputDefinitions,
				loadedOutputDefinitions,
			)
		}
	}

	return nil
}

func getOutputDefinitionsFromProto(outputs []*gen.Output) ([]string, error) {
	outputDefinitions := make([]string, 0, len(outputs))
	for outputIndex, output := range outputs {
		outputDefinition, err := getOutputDefinitionFromProto(output)
		if err != nil {
			return nil, fmt.Errorf("entry %d: %w", outputIndex, err)
		}
		outputDefinitions = append(outputDefinitions, outputDefinition)
	}
	return outputDefinitions, nil
}

func getOutputDefinitionFromProto(output *gen.Output) (string, error) {
	if output == nil {
		return "", fmt.Errorf("output is nil")
	}

	switch outputKind := output.Kind.(type) {
	case *gen.Output_File:
		return model.NewOutput(string(handlers.FileHandler), outputKind.File.GetPath()).String(), nil
	case *gen.Output_Directory:
		return model.NewOutput(string(handlers.DirHandler), outputKind.Directory.GetPath()).String(), nil
	case *gen.Output_DockerImage:
		return model.NewOutput(string(handlers.DockerHandler), outputKind.DockerImage.GetLocalTag()).String(), nil
	default:
		return "", fmt.Errorf("unknown output kind: %T", output.Kind)
	}
}
