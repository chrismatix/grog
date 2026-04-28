package execution

import (
	"context"
	"errors"
	"fmt"
	"grog/internal/caching"
	"grog/internal/config"
	"grog/internal/console"
	"grog/internal/dag"
	"grog/internal/hashing"
	"grog/internal/label"
	"grog/internal/model"
	"grog/internal/output"
	"grog/internal/output/handlers"
	"grog/internal/proto/gen"
	"grog/internal/worker"
	"os"
	"path/filepath"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/fatih/color"
	"google.golang.org/protobuf/encoding/protojson"
)

type CommandError struct {
	TargetLabel label.TargetLabel
	ExitCode    int
	Output      string
}

func (e *CommandError) Error() string {
	return fmt.Sprintf("target %s failed with exit code %d: %s", e.TargetLabel, e.ExitCode, e.Output)
}

type Executor struct {
	targetCache      *caching.TargetResultCache
	taintCache       *caching.TaintCache
	registry         *output.Registry
	graph            *dag.DirectedTargetGraph
	failFast         bool
	enableCache      bool
	loadOutputsMode  config.LoadOutputsMode
	targetHasher     *hashing.TargetHasher
	streamLogsToggle *console.StreamLogsToggle
	coordinator      *PoolCoordinator
	scheduler        *Scheduler
	cacheWriter      *CacheWriter
	asyncWaitTime    time.Duration
	deferAsyncWait   bool
	asyncDrained     bool
}

func NewExecutor(
	targetCache *caching.TargetResultCache,
	taintCache *caching.TaintCache,
	registry *output.Registry,
	graph *dag.DirectedTargetGraph,
	failFast bool,
	streamLogs bool,
	enableCache bool,
	loadOutputsMode config.LoadOutputsMode,
) *Executor {
	return &Executor{
		targetCache:      targetCache,
		taintCache:       taintCache,
		registry:         registry,
		graph:            graph,
		failFast:         failFast,
		enableCache:      enableCache,
		loadOutputsMode:  loadOutputsMode,
		targetHasher:     hashing.NewTargetHasher(graph),
		streamLogsToggle: console.NewStreamLogsToggle(streamLogs),
	}
}

// Execute executes the targets in the given graph and returns the completion map
func (e *Executor) Execute(ctx context.Context) (dag.CompletionMap, error) {
	numWorkers := config.Global.NumWorkers
	numIOWorkers := config.Global.NumIOWorkers
	if numIOWorkers < 1 {
		numIOWorkers = numWorkers * 3
	}
	stdLogger := console.GetLogger(ctx)

	ctx = console.WithStreamLogsToggle(ctx, e.streamLogsToggle)
	ctx, program, sendMsg := console.StartTaskUI(ctx)
	defer func(p *tea.Program) {
		err := p.ReleaseTerminal()
		if err != nil {
			stdLogger.Errorf("error releasing terminal: %v", err)
		}
	}(program)
	defer program.Quit()

	// Get selected nodes and create a TestLogger for them
	selectedNodes := e.graph.GetSelectedNodes()
	selectedNodeCount := len(selectedNodes)

	// Create a list of node labels for the TestLogger
	var targetLabels []string
	for _, node := range selectedNodes {
		if target, ok := node.(*model.Target); ok && target.IsTest() {
			targetLabels = append(targetLabels, node.GetLabel().String())
		}
	}

	// Create a TestLogger with the node labels
	testLogger := console.NewTestLogger(targetLabels, 0) // 0 means use default terminal width

	// Add the TestLogger to the context
	ctx = context.WithValue(ctx, console.TestLoggerKey{}, testLogger)

	coordinator := NewPoolCoordinator(stdLogger, numWorkers, numIOWorkers, sendMsg, selectedNodeCount)
	coordinator.StartTaskWorkers(ctx)
	defer coordinator.Shutdown()
	e.coordinator = coordinator
	e.scheduler = NewScheduler(coordinator.TaskPool())

	ioContext := context.WithoutCancel(ctx)
	coordinator.StartIOWorkers(ioContext)
	e.cacheWriter = NewCacheWriter(e.targetCache, coordinator.IOPool(), config.Global.AsyncCacheWrites, ioContext)

	// walkCallback will be called at max parallelism by the graph walker
	walkCallback := func(ctx context.Context, node model.BuildNode) (dag.CacheResult, error) {
		target, ok := node.(*model.Target)
		if !ok {
			// this is where we would add the execution of other node types
			return dag.CacheHit, nil
		}

		// get any possible bin tools that the node may use
		binTools, err := e.getBinToolPaths(target)
		if err != nil {
			return dag.CacheMiss, err
		}

		outputIdentifiers := e.getDependencyOutputIdentifiers(target)

		hashStart := time.Now()
		err = e.targetHasher.SetTargetChangeHash(target)
		target.HashDuration = time.Since(hashStart)
		if err != nil {
			return dag.CacheMiss, err
		}

		queuedAt := time.Now()
		// taskFunc will be run in the worker pool, gated by the scheduler on
		// any concurrency group membership.
		taskFunc := e.getTaskFunc(ctx, target, binTools, outputIdentifiers, queuedAt)
		return e.scheduler.Schedule(ctx, target, taskFunc)
	}

	walker := dag.NewWalker(e.graph, walkCallback, e.failFast)
	completionMap, err := walker.Walk(ctx)

	// Drain the I/O pool before returning, but only if the build was not
	// interrupted and the caller hasn't opted to wait itself. Async cache
	// writes use a non-cancellable context so they can outlive the build,
	// but we must not block an interrupted shutdown on slow remote uploads.
	if ctx.Err() == nil && !e.deferAsyncWait {
		e.drainAsyncWrites()
	}

	return completionMap, err
}

// DeferAsyncWait opts out of the implicit cache-write wait in Execute so the
// caller can run other work in parallel with uploads, then call
// WaitForAsyncWrites when ready.
func (e *Executor) DeferAsyncWait() {
	e.deferAsyncWait = true
}

// WaitForAsyncWrites blocks until all outstanding async cache writes complete,
// recording the elapsed time as AsyncWaitTime. Idempotent — safe to call
// whether or not Execute already waited. Logs a status line when writes are
// still in flight so the user understands the wait.
func (e *Executor) WaitForAsyncWrites(ctx context.Context) {
	if e.asyncDrained || e.cacheWriter == nil || ctx.Err() != nil {
		return
	}
	if e.cacheWriter.HasPendingWrites() {
		console.GetLogger(ctx).Infof("Waiting for cache writes to finish in the background...")
	}
	e.drainAsyncWrites()
}

func (e *Executor) drainAsyncWrites() {
	waitStart := time.Now()
	e.cacheWriter.Wait()
	e.asyncWaitTime = time.Since(waitStart)
	e.asyncDrained = true
}

// AsyncWaitTime returns the wall-clock time spent waiting for the async I/O
// pool to drain after the DAG walk completed. Zero when async writes are
// disabled or the build was interrupted.
func (e *Executor) AsyncWaitTime() time.Duration {
	return e.asyncWaitTime
}

// getBinToolPaths From all the direct dependencies of a target, get their bin_output if defined
func (e *Executor) getBinToolPaths(target *model.Target) (BinToolMap, error) {
	deps := e.graph.GetTargetDependencies(target)

	binTools := make(map[string]string, 0)
	for _, dep := range deps {
		if dep.HasBinOutput() {
			// Say a target in pkg foo/bar defines a bin output pointing to ../dist/bin.exe
			// then we want to resolve it to /workspace_path/foo/dist/bin.exe
			binToolPath := config.GetPathAbsoluteToWorkspaceRoot(filepath.Join(dep.Label.Package, dep.BinOutput.Identifier))

			binTools[dep.Label.String()] = binToolPath
			// If the dependency is in the same package we want to allow the shorthand
			// syntax for invoking the tool, i.e. $(bin :tool)
			if dep.Label.Package == target.Label.Package {
				binTools[":"+dep.Label.Name] = binToolPath
			}
			// Likewise allow for the //foo:foo -> //foo shorthand
			if dep.Label.CanBeShortened() {
				binTools["//"+dep.Label.Package] = binToolPath
			}
		}
	}
	return binTools, nil
}

// getDependencyOutputIdentifiers builds the output map available to the shell
// helper functions for a target's direct dependencies.
func (e *Executor) getDependencyOutputIdentifiers(target *model.Target) OutputIdentifierMap {
	deps := e.graph.GetTargetDependencies(target)
	outputIdentifiers := make(OutputIdentifierMap, 0)

	for _, dep := range deps {
		depOutputs := getTargetOutputIdentifiers(dep)
		if len(depOutputs) == 0 {
			continue
		}

		outputIdentifiers[dep.Label.String()] = depOutputs
		if dep.Label.Package == target.Label.Package {
			outputIdentifiers[":"+dep.Label.Name] = depOutputs
		}
		if dep.Label.CanBeShortened() {
			outputIdentifiers["//"+dep.Label.Package] = depOutputs
		}
	}

	return outputIdentifiers
}

func getTargetOutputIdentifiers(target *model.Target) []string {
	var identifiers []string
	for _, targetOutput := range target.AllOutputs() {
		if !targetOutput.IsSet() {
			continue
		}
		if targetOutput.Type == string(handlers.FileHandler) || targetOutput.Type == string(handlers.DirHandler) {
			workspaceRelativePath := filepath.Join(target.Label.Package, targetOutput.Identifier)
			identifiers = append(identifiers, config.GetPathAbsoluteToWorkspaceRoot(workspaceRelativePath))
			continue
		}
		identifiers = append(identifiers, targetOutput.Identifier)
	}
	return identifiers
}

func (e *Executor) getTaskFunc(
	ctx context.Context,
	target *model.Target,
	binToolPaths BinToolMap,
	outputIdentifiers OutputIdentifierMap,
	queuedAt time.Time,
) worker.TaskFunc[dag.CacheResult] {
	return func(update worker.StatusFunc) (dag.CacheResult, error) {
		startTime := time.Now()
		target.StartTime = startTime
		target.QueueWait = startTime.Sub(queuedAt)

		logger := console.GetLogger(ctx)
		update(worker.Status(fmt.Sprintf("%s: checking cache.", target.Label)))

		cacheCheckStart := time.Now()
		targetResult, err := e.targetCache.Load(ctx, target.ChangeHash)
		if err != nil {
			// TODO distinguish between NotFound and cache backend errors
			// logger.Warnf("failed to check target %s cache: %v", target.Label, err)
		}
		target.HasCacheHit = targetResult != nil
		if logger.DebugEnabled() {
			logger.Debugf("%s: loaded target result %s", target.Label, formatTargetResultForDebug(targetResult))
		}

		outputCheckErr := runOutputChecks(ctx, target, binToolPaths, outputIdentifiers)
		if outputCheckErr != nil {
			logger.Debugf("running target due to output check error: %v", outputCheckErr)
		}

		// Check if the target is tainted
		isTainted, taintedErr := e.taintCache.IsTainted(ctx, target.Label)
		if taintedErr != nil {
			logger.Errorf("Failed to check if target %s is tainted: %v", target.Label, taintedErr)
		}
		target.CacheCheckTime = time.Since(cacheCheckStart)

		if isTainted {
			logger.Debugf("running target %s due to being tainted", target.Label)
		}

		// Process a cache hit if:
		// - The target result was loaded (HasCacheHit)
		// - The target is not tainted (!isTainted)
		// - The target does not have no-cache set (!target.SkipsCache)
		// - The cache is enabled (enableCache)
		if target.HasCacheHit && !isTainted && !target.SkipsCache() && e.enableCache {
			if e.loadOutputsMode == config.LoadOutputsMinimal {
				// Important: Set the output hash so that descendants can compute their change hashes
				target.OutputHash = targetResult.OutputHash
				update(worker.Status(fmt.Sprintf("%s: cache hit. skipped loading %s because load_outputs=minimal.", target.Label, console.FCountOutputs(len(target.AllOutputs())))))
				logger.Debugf("%s: cache hit. skipped loading %s because load_ outputs=minimal", target.Label, console.FCountOutputs(len(target.AllOutputs())))
				return dag.CacheHit, nil
			}

			update(worker.Status(fmt.Sprintf("%s: cache hit. loading %s.", target.Label, console.FCountOutputs(len(target.AllOutputs())))))
			progress := worker.NewProgressTracker(
				fmt.Sprintf("%s: loading %s", target.Label, console.FCountOutputs(len(target.AllOutputs()))),
				0,
				update,
			)

			outputLoadStart := time.Now()
			loadingErr := e.registry.LoadOutputs(ctx, target, targetResult, progress)
			target.OutputLoadTime = time.Since(outputLoadStart)
			target.CacheTime += target.OutputLoadTime
			if loadingErr != nil {
				// Don't return so that we instead break out and continue executing the target
				logger.Errorf("%s re-running due to output loading failure: %v", target.Label, loadingErr)
			} else {
				if target.IsTest() {
					executionTime := time.Since(startTime).Seconds()
					if testLogger := console.GetTestLogger(ctx); testLogger != nil {
						// Log the cached execution time here
						testLogger.LogTestPassedCached(logger, target.Label.String(), float64(targetResult.ExecutionDurationMillis)/1000)
					} else {
						logger.Infof("%s %s (cached) in %.1fs", target.Label, color.New(color.FgGreen).Sprintf("PASSED"), executionTime)
					}
				}
				return dag.CacheHit, nil
			}
		}

		if !target.HasCacheHit {
			logger.Debugf("running target %s due to cache miss", target.Label)
		}
		if outputCheckErr != nil {
			logger.Debugf("running target %s due to output check error", target.Label)
		}

		if e.loadOutputsMode == config.LoadOutputsMinimal {
			update(worker.Status(fmt.Sprintf("%s: loading dependency outputs (load_outputs=minimal).", target.Label)))
			depLoadStart := time.Now()
			if loadDepsErr := e.LoadDependencyOutputs(ctx, target, update); loadDepsErr != nil {
				return dag.CacheMiss, fmt.Errorf("failed to load dependency outputs for target %s: %w", target.Label, loadDepsErr)
			}
			target.DepLoadTime = time.Since(depLoadStart)
		}

		return e.executeTarget(ctx, target, binToolPaths, outputIdentifiers, update, isTainted)
	}
}

func formatTargetResultForDebug(targetResult *gen.TargetResult) string {
	if targetResult == nil {
		return "<nil>"
	}

	marshalledResult, err := protojson.MarshalOptions{
		EmitUnpopulated: true,
		UseProtoNames:   true,
	}.Marshal(targetResult)
	if err != nil {
		return fmt.Sprintf("%v", targetResult)
	}

	return string(marshalledResult)
}

func (e *Executor) executeTarget(
	ctx context.Context,
	target *model.Target,
	binToolPaths BinToolMap,
	outputIdentifiers OutputIdentifierMap,
	update worker.StatusFunc,
	isTainted bool,
) (dag.CacheResult, error) {
	logger := console.GetLogger(ctx)

	startTime := time.Now()
	var err error
	if target.Command != "" {
		update(worker.Status(fmt.Sprintf("%s: \"%s\"", target.Label, target.CommandEllipsis())))
		logger.Debugf("running target %s: %s", target.Label, target.CommandEllipsis())
		err = executeTarget(ctx, target, binToolPaths, outputIdentifiers, e.streamLogsToggle.Enabled())
	} else {
		logger.Debugf("skipped target %s due to no command", target.Label)
	}
	target.ExecutionTime = time.Since(startTime)
	executionTimeSeconds := target.ExecutionTime.Seconds()

	if err != nil {
		logger.Debugf("target execution returned error %s: %s", target.Label, err)
		if target.IsTest() && !errors.Is(err, context.Canceled) {
			// Test errors we want to log differently
			// Cancellations should just exit the execution
			if testLogger := console.GetTestLogger(ctx); testLogger != nil {
				testLogger.LogTestFailed(logger, target.Label.String(), target.ExecutionTime)
			} else {
				logger.Infof("%s %s in %.1fs", target.Label, color.New(color.FgRed).Sprintf("FAILED"), executionTimeSeconds)
			}
		}
		return dag.CacheMiss, err
	}

	// Run output checks again to see if they match now
	if outputCheckErr := runOutputChecks(ctx, target, binToolPaths, outputIdentifiers); outputCheckErr != nil {
		return dag.CacheMiss, outputCheckErr
	}

	if target.IsTest() {
		if testLogger := console.GetTestLogger(ctx); testLogger != nil {
			testLogger.LogTestPassed(logger, target.Label.String(), executionTimeSeconds)
		} else {
			logger.Infof("%s %s in %.1fs", target.Label, color.New(color.FgGreen).Sprintf("PASSED"), executionTimeSeconds)
		}
	}

	// If the target produced a bin output automatically mark it as executable
	err = markBinOutputExecutable(target)
	if err != nil {
		return dag.CacheMiss, err
	}

	// Write outputs to the cache:
	logger.Debugf("writing outputs for target %s", target.Label)
	update(worker.Status(fmt.Sprintf("%s complete. writing outputs...", target.Label)))
	err = e.OnTargetComplete(ctx, target, update)
	if err != nil {
		return dag.CacheMiss, fmt.Errorf("build completed but failed to write outputs to cache for target %s:\n%w", target.Label, err)
	}

	if isTainted {
		go func() {
			err = e.taintCache.Clear(ctx, target.Label)
			if err != nil {
				logger.Errorf("Failed to remove taint from target %s: %v", target.Label, err)
			}
		}()
	}

	return dag.CacheMiss, nil
}

// OnTargetComplete should be called when a target has completed executing
// - writes the outputs if necessary
// - computes and sets the output hash
// - writes the target result to the cache
// For no-cache targets it will set the OutputHash to the hash of the outputs
func (e *Executor) OnTargetComplete(ctx context.Context, target *model.Target, update worker.StatusFunc) error {
	logger := console.GetLogger(ctx)
	var targetResult *gen.TargetResult
	var preparedTarget *output.PreparedTargetResult
	var err error

	if target.SkipsCache() || !e.enableCache {
		logger.Debugf("%s: skipping cache write", target.Label)
		targetResult, err = e.registry.GetNoCacheOutputHash(ctx, target)
		if err == nil {
			preparedTarget = &output.PreparedTargetResult{
				TargetResult: targetResult,
			}
		}
		// TODO should we even store this in the cache given that the target
		// is no-cache? Probably fine from a user perspective
		// since it's the target cache and not the output cache
	} else if len(target.AllOutputs()) == 0 {
		// NOTE: This is a special and intentional design
		// Targets that do not have any outputs expose their own change behavior as an output
		// analogous to file_groups
		logger.Debugf("%s: no outputs to write", target.Label)
		preparedTarget = &output.PreparedTargetResult{
			TargetResult: &gen.TargetResult{
				ChangeHash:              target.ChangeHash,
				OutputHash:              target.ChangeHash,
				ExecutionDurationMillis: target.ExecutionTime.Milliseconds(),
			},
		}
	} else {
		logger.Debugf("%s: writing %d outputs", target.Label, len(target.AllOutputs()))
		progress := worker.NewProgressTracker(
			fmt.Sprintf("%s: writing %s", target.Label, console.FCountOutputs(len(target.AllOutputs()))),
			0,
			update,
		)
		outputWriteStart := time.Now()
		writeResult, writeErr := e.registry.PrepareOutputs(ctx, target, progress)
		target.OutputWriteTime = time.Since(outputWriteStart)
		target.CacheTime += target.OutputWriteTime
		if writeErr != nil {
			return writeErr
		}
		preparedTarget = writeResult
	}
	if err != nil {
		return err
	}
	if preparedTarget == nil {
		return fmt.Errorf("prepared target result for %s is nil", target.Label)
	}
	targetResult = preparedTarget.TargetResult

	target.OutputsLoaded = true
	target.OutputHash = targetResult.OutputHash

	cacheWriteStart := time.Now()
	defer func() {
		target.CacheWriteTime = time.Since(cacheWriteStart)
		target.CacheTime += target.CacheWriteTime
	}()

	return e.cacheWriter.PersistPreparedTarget(ctx, target.Label.String(), preparedTarget, update)
}

// LoadDependencyOutputs is used to load the outputs of the targets that a target depends on.
// Since there is a chance that the loading will fail it needs to be able to recursively re-run targets.
// Primarily used for the load_outputs=minimal mode which will avoid loading outputs until necessary.
func (e *Executor) LoadDependencyOutputs(
	ctx context.Context,
	target *model.Target,
	update worker.StatusFunc,
) error {
	logger := console.GetLogger(ctx)
	logger.Debugf(
		"loading dependency outputs for target %s.",
		target.Label,
	)
	for _, dep := range e.graph.GetTargetDependencies(target) {
		localDep := dep
		// Function to re-run a dependency in case we
		rerunDependency := func() error {
			binTools, binToolErr := e.getBinToolPaths(localDep)
			if binToolErr != nil {
				return binToolErr
			}

			outputIdentifiers := e.getDependencyOutputIdentifiers(localDep)

			update(worker.Status(fmt.Sprintf("%s: re-running dependency %s (load_outputs_mode=minimal).", target.Label, localDep.Label)))
			_, executionErr := e.executeTarget(ctx, localDep, binTools, outputIdentifiers, update, false)
			if executionErr != nil {
				return executionErr
			}
			return nil
		}

		targetResult, err := e.targetCache.Load(ctx, localDep.ChangeHash)

		var loadErr error
		if err == nil {
			progress := worker.NewProgressTracker(
				fmt.Sprintf("%s: loading %s", target.Label, console.FCountOutputs(len(target.AllOutputs()))),
				0,
				update,
			)
			loadErr = e.registry.LoadOutputs(ctx, localDep, targetResult, progress)
		} else {
			// Target cache not available (e.g. async cache write not yet complete).
			// Treat as a load failure so the dependency is re-built below.
			loadErr = err
		}

		if loadErr != nil || localDep.SkipsCache() {
			logger.Debugf(
				"%s: failed to load output for dependency %s (re-running): loadErr=%v no-cache=%t",
				target.Label,
				localDep.Label,
				loadErr,
				localDep.SkipsCache(),
			)
			// In this case we need to also recursively re-load the dependencies of the dependency
			if recursiveLoadErr := e.LoadDependencyOutputs(ctx, localDep, update); recursiveLoadErr != nil {
				return recursiveLoadErr
			}

			if rerunError := rerunDependency(); rerunError != nil {
				return rerunError
			}
		}
	}

	return nil
}

func markBinOutputExecutable(target *model.Target) error {
	if !target.HasBinOutput() {
		return nil
	}

	binOutputPath := config.GetPathAbsoluteToWorkspaceRoot(filepath.Join(target.Label.Package, target.BinOutput.Identifier))
	err := os.Chmod(binOutputPath, 0755)
	if err != nil {
		return fmt.Errorf("failed to mark binary output as executable for target %s: %w", target.Label, err)
	}

	return nil
}
