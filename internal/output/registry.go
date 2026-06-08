package output

import (
	"context"
	"errors"
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
	"io"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"sync"

	"github.com/alitto/pond/v2"
)

// PreparedTargetResult contains the target result and any prepared write plans.
type PreparedTargetResult struct {
	TargetResult *gen.TargetResult
	WritePlans   []handlers.OutputWritePlan
}

// Registry manages the available output handlers.
type Registry struct {
	handlers     map[string]handlers.Handler
	cas          *caching.Cas
	pool         pond.Pool
	handlerMutex sync.RWMutex

	// Features like load_outputs=minimal may load outputs concurrently
	// In this case we want to make sure that only happens once per target
	targetMutexMap *maps.MutexMap

	hashMutex       sync.RWMutex
	hashCache       map[string]string
	outputHashMutex sync.RWMutex
	outputHashCache map[string]map[model.Output]string

	pushReporter *handlers.PushReporter
	pushEnabled  func() bool
	imagePusher  handlers.ImagePusher
}

// NewRegistry creates a new registry with default handlers.
func NewRegistry(
	ctx context.Context,
	cas *caching.Cas,
) *Registry {
	pushReporter := handlers.NewPushReporter(func() bool { return config.Global.FailFast })
	pushEnabled := func() bool { return config.Global.Push }

	r := &Registry{
		handlers:        make(map[string]handlers.Handler),
		targetMutexMap:  maps.NewMutexMap(),
		hashCache:       make(map[string]string),
		outputHashCache: make(map[string]map[model.Output]string),
		pool: pond.NewPool(
			runtime.NumCPU() * 2,
		),
		pushReporter: pushReporter,
	}

	r.Register(handlers.NewFileOutputHandler(cas))
	r.Register(handlers.NewDirectoryOutputHandler(cas))

	var ociHandler handlers.Handler
	if config.Global.OCI.Backend == "registry" {
		ociHandler = handlers.NewDockerRegistryOutputHandler(cas, config.Global.OCI)
	} else {
		ociHandler = handlers.NewDockerOutputHandler(ctx, cas, config.Global.OCI.InsecureRegistries)
	}
	r.Register(ociHandler)
	// The oci handler also pushes images; record the ImagePusher facet so
	// the per-target push hook can find it without re-traversing handlers.
	pusher, ok := ociHandler.(handlers.ImagePusher)
	if !ok {
		panic(fmt.Sprintf("oci handler %T does not implement ImagePusher", ociHandler))
	}
	r.imagePusher = pusher
	r.pushEnabled = pushEnabled
	return r
}

// buildPushPlans assembles one push plan per (oci output, destination) for
// every entry in target.OciPush. Returns an error if a key references a name
// that is not produced by the target's oci:: outputs — that's a recipe bug.
func (r *Registry) buildPushPlans(target *model.Target, outputs []*gen.Output) ([]handlers.OutputWritePlan, error) {
	if len(target.OciPush) == 0 {
		return nil, nil
	}
	var plans []handlers.OutputWritePlan
	for localName, destinations := range target.OciPush {
		ociImage := findOciImageByLocalTag(outputs, localName)
		if ociImage == nil {
			return nil, fmt.Errorf("%s: oci_push key %q does not match any oci:: output", target.Label, localName)
		}
		for _, dest := range destinations {
			plans = append(plans, handlers.NewOciPushPlan(
				r.imagePusher, ociImage, dest, target.Label.String(), r.pushReporter,
			))
		}
	}
	return plans, nil
}

func findOciImageByLocalTag(outputs []*gen.Output, localTag string) *gen.OCIImageOutput {
	for _, out := range outputs {
		img := out.GetOciImage()
		if img == nil {
			continue
		}
		if img.GetLocalTag() == localTag {
			return img
		}
	}
	return nil
}

func (r *Registry) PushReporter() *handlers.PushReporter {
	return r.pushReporter
}

// Close releases resources held by output handlers (notably the in-process
// loopback Docker registry). Must be called only after all async cache writes
// have drained — otherwise an in-flight `docker push` may race the proxy
// shutdown and fail with "connection refused".
func (r *Registry) Close() error {
	r.handlerMutex.RLock()
	defer r.handlerMutex.RUnlock()

	var errs []error
	for _, h := range r.handlers {
		closer, ok := h.(io.Closer)
		if !ok {
			continue
		}
		if err := closer.Close(); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

// Register adds a new output handler to the registry.
func (r *Registry) Register(handler handlers.Handler) {
	r.handlerMutex.Lock()
	defer r.handlerMutex.Unlock()

	if _, ok := r.handlers[string(handler.Type())]; ok {
		panic(fmt.Sprintf("handler for type %s already registered", handler.Type()))
	}
	r.handlers[string(handler.Type())] = handler
}

// GetHandler retrieves a handler by type.
func (r *Registry) mustGetHandlerFromProto(output *gen.Output) handlers.Handler {
	var outputType string
	switch output.Kind.(type) {
	case *gen.Output_File:
		outputType = string(handlers.FileHandler)
	case *gen.Output_Directory:
		outputType = string(handlers.DirHandler)
	case *gen.Output_OciImage:
		outputType = string(handlers.OCIHandler)
	default:
		panic(fmt.Errorf("unknown output kind: %T", output.Kind))
	}
	return r.mustGetHandler(outputType)
}

// GetHandler retrieves a handler by type.
func (r *Registry) mustGetHandler(outputType string) handlers.Handler {
	r.handlerMutex.RLock()
	defer r.handlerMutex.RUnlock()

	handler, exists := r.handlers[outputType]
	if !exists {
		panic(fmt.Sprintf("handler for type %s not registered", outputType))
	}
	return handler
}

func (r *Registry) PrepareOutputs(
	ctx context.Context,
	target *model.Target,
	progress *worker.ProgressTracker,
) (*PreparedTargetResult, error) {
	r.targetMutexMap.Lock(target.Label.String())
	defer r.targetMutexMap.Unlock(target.Label.String())

	logger := console.GetLogger(ctx)
	outputs := target.AllOutputs()

	logger.Debugf("%s: writing outputs", target.Label)

	var tasks []pond.Task
	var preparedOutputs []*handlers.PreparedOutput
	var outputsMutex sync.Mutex

	for _, outputRef := range outputs {
		localOutputRef := outputRef
		task := r.pool.SubmitErr(func() error {
			result, err := r.mustGetHandler(localOutputRef.Type).Write(ctx, *target, localOutputRef, progress)
			if err != nil {
				return err
			}
			outputsMutex.Lock()
			preparedOutputs = append(preparedOutputs, result)
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

	// Extract outputs and collect write plans.
	targetOutputs := make([]*gen.Output, 0, len(preparedOutputs))
	var writePlans []handlers.OutputWritePlan
	for _, preparedOutput := range preparedOutputs {
		targetOutputs = append(targetOutputs, preparedOutput.Output)
		if preparedOutput.WritePlan != nil {
			writePlans = append(writePlans, preparedOutput.WritePlan)
		}
	}

	// Append a push plan per (oci output, destination) for any oci_push entry
	// declared on the target. Each plan runs after the cache plans so that
	// image_id and manifest_digest are populated before it tries to source.
	if r.pushEnabled != nil && r.pushEnabled() {
		pushPlans, err := r.buildPushPlans(target, targetOutputs)
		if err != nil {
			return nil, err
		}
		writePlans = append(writePlans, pushPlans...)
	}

	outputHash, err := getOutputHash(targetOutputs)
	if err != nil {
		return nil, err
	}

	if err := ensureBinOutputExecutable(target); err != nil {
		return nil, err
	}

	logger.Debugf("%s: outputs written", target.Label)

	return &PreparedTargetResult{
		TargetResult: &gen.TargetResult{
			ChangeHash:              target.ChangeHash,
			OutputHash:              outputHash,
			Outputs:                 targetOutputs,
			ExecutionDurationMillis: target.ExecutionTime.Milliseconds(),
		},
		WritePlans: writePlans,
	}, nil
}

// GetNoCacheOutputHash computes the output hash for a target when target caching is disabled
// using handler.GetHash() on local resources only.
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

	if err := ensureBinOutputExecutable(target); err != nil {
		return nil, err
	}

	return &gen.TargetResult{
		ChangeHash:              target.ChangeHash,
		OutputHash:              hashing.HashStrings(digests),
		ExecutionDurationMillis: target.ExecutionTime.Milliseconds(),
	}, nil
}

// LoadOutputs loads the outputs for a target once using the cached targetResult.
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

	if err := ensureBinOutputExecutable(target); err != nil {
		return err
	}

	// Fire pushes for every oci_push entry on the current target. The cached
	// proto only carries the local image; destinations come from the live
	// target. Push errors are recorded to the reporter but never propagated
	// — a transient registry hiccup must not invalidate a cache restore.
	if r.pushEnabled != nil && r.pushEnabled() && len(target.OciPush) > 0 {
		plans, err := r.buildPushPlans(target, targetResult.Outputs)
		if err != nil {
			return err
		}
		for _, plan := range plans {
			_ = plan.Execute(ctx, progress)
		}
	}

	logger.Debugf("%s: outputs loaded", target.Label)
	target.OutputsLoaded = true
	target.OutputHash = targetResult.OutputHash
	return nil
}

// SeedLayerCaches calls SeedLayerCache on any handler that implements
// LayerCacheSeeder for the target's outputs. This pulls previous images
// into the local Docker daemon before a build so their layers can be reused.
// SeedLayerCaches calls SeedLayerCache on any handler that implements
// LayerCacheSeeder for the target's outputs. This pulls prior images
// into the local Docker daemon before a build so their layers can be reused.
// priorChangeHash identifies the same target's content at an earlier git ref
// and is forwarded to the seeder to locate the immutable cache image to pull.
func (r *Registry) SeedLayerCaches(
	ctx context.Context,
	target *model.Target,
	priorChangeHash string,
	update worker.StatusFunc,
) {
	logger := console.GetLogger(ctx)

	// Deduplicate handler types — only seed once per handler even if a target
	// has multiple docker outputs.
	seeded := make(map[handlers.HandlerType]bool)
	for _, outputRef := range target.AllOutputs() {
		handler := r.mustGetHandler(outputRef.Type)
		if seeded[handler.Type()] {
			continue
		}
		seeder, ok := handler.(handlers.LayerCacheSeeder)
		if !ok {
			continue
		}
		seeded[handler.Type()] = true

		update(worker.Status(fmt.Sprintf("%s: seeding layer cache", target.Label)))
		progress := worker.NewProgressTracker(
			fmt.Sprintf("%s: seeding layer cache", target.Label),
			0,
			update,
		)
		if err := seeder.SeedLayerCache(ctx, *target, priorChangeHash, progress); err != nil {
			// Layer cache seeding is best-effort; log and continue.
			logger.Debugf("%s: layer cache seed failed (non-fatal): %v", target.Label, err)
		}
	}
}

// ensureBinOutputExecutable marks a target's bin_output 0755 on disk.
// The CAS doesn't track file mode, and a user's build command may not chmod
// the binary itself, so the Registry guarantees this post-condition at every
// boundary that produces outputs (fresh build, no-cache build, cache restore)
// — see issue #155.
func ensureBinOutputExecutable(target *model.Target) error {
	if !target.HasBinOutput() {
		return nil
	}
	binOutputPath := config.GetPathAbsoluteToWorkspaceRoot(
		filepath.Join(target.Label.Package, target.BinOutput.Identifier),
	)
	if err := os.Chmod(binOutputPath, 0755); err != nil {
		return fmt.Errorf("failed to mark bin_output executable for %s: %w", target.Label, err)
	}
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
	case *gen.Output_OciImage:
		return model.NewOutput(string(handlers.OCIHandler), outputKind.OciImage.GetLocalTag()).String(), nil
	default:
		return "", fmt.Errorf("unknown output kind: %T", output.Kind)
	}
}
