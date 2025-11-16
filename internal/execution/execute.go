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
	"grog/internal/worker"
	"os"
	"path/filepath"
	"sync/atomic"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/fatih/color"
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
	targetCache      *caching.TargetCache
	registry         *output.Registry
	graph            *dag.DirectedTargetGraph
	failFast         bool
	enableCache      bool
	loadOutputsMode  config.LoadOutputsMode
	targetHasher     *hashing.TargetHasher
	streamLogsToggle *console.StreamLogsToggle
	execDurationNs   atomic.Int64
}

// Stats capture aggregated executor metrics.
type Stats struct {
	ExecDuration  time.Duration
	CacheDuration time.Duration
}

func (e *Executor) addExecDuration(duration time.Duration) {
	if duration <= 0 {
		return
	}
	e.execDurationNs.Add(duration.Nanoseconds())
}

func NewExecutor(
	targetCache *caching.TargetCache,
	registry *output.Registry,
	graph *dag.DirectedTargetGraph,
	failFast bool,
	streamLogs bool,
	loadOutputsMode config.LoadOutputsMode,
) *Executor {
	return &Executor{
		targetCache:      targetCache,
		registry:         registry,
		graph:            graph,
		failFast:         failFast,
		loadOutputsMode:  loadOutputsMode,
		targetHasher:     hashing.NewTargetHasher(graph),
		streamLogsToggle: console.NewStreamLogsToggle(streamLogs),
	}
}

// Execute executes the targets in the given graph and returns the completion map
func (e *Executor) Execute(ctx context.Context) (dag.CompletionMap, Stats, error) {
	numWorkers := config.Global.NumWorkers
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

	workerPool := worker.NewTaskWorkerPool[dag.CacheResult](stdLogger, numWorkers, sendMsg, selectedNodeCount)
	workerPool.StartWorkers(ctx)
	defer workerPool.Shutdown()

	// walkCallback will be called at max parallelism by the graph walker
	walkCallback := func(ctx context.Context, node model.BuildNode, depsCached bool) (dag.CacheResult, error) {
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

		err = e.targetHasher.SetTargetChangeHash(target)
		if err != nil {
			return dag.CacheMiss, err
		}

		// taskFunc will be run in the worker pool
		taskFunc := e.getTaskFunc(ctx, target, binTools, outputIdentifiers, depsCached)
		return workerPool.Run(taskFunc)
	}

	walker := dag.NewWalker(e.graph, walkCallback, e.failFast)
	completionMap, err := walker.Walk(ctx)
	stats := Stats{
		ExecDuration:  time.Duration(e.execDurationNs.Load()),
		CacheDuration: e.registry.CacheDuration(),
	}
	return completionMap, stats, err
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
	for _, output := range target.AllOutputs() {
		if !output.IsSet() {
			continue
		}
		if output.Type == string(handlers.FileHandler) || output.Type == string(handlers.DirHandler) {
			workspaceRelativePath := filepath.Join(target.Label.Package, output.Identifier)
			identifiers = append(identifiers, config.GetPathAbsoluteToWorkspaceRoot(workspaceRelativePath))
			continue
		}
		identifiers = append(identifiers, output.Identifier)
	}
	return identifiers
}

func (e *Executor) getTaskFunc(
	ctx context.Context,
	target *model.Target,
	binToolPaths BinToolMap,
	outputIdentifiers OutputIdentifierMap,
	depsCached bool,
) worker.TaskFunc[dag.CacheResult] {
	return func(update worker.StatusFunc) (dag.CacheResult, error) {
		startTime := time.Now()

		update(fmt.Sprintf("%s: checking cache.", target.Label))
		hasCacheHit, err := e.registry.HasCacheHit(ctx, target)
		if err != nil {
			return dag.CacheMiss, err
		}
		target.HasCacheHit = hasCacheHit

		logger := console.GetLogger(ctx)

		outputCheckErr := runOutputChecks(ctx, target, binToolPaths, outputIdentifiers)
		if outputCheckErr != nil {
			logger.Debugf("running target due to output check error: %v", outputCheckErr)
		} else if depsCached == true && target.HasOutputChecksOnly() {
			// Special type of target that only has output checks but no in- and outputs
			// In this case the output checks (and the dependencies) are the only thing affecting re-running
			logger.Debugf("skipping target %s due to passed output checks", target.Label)
			return dag.CacheSkip, nil
		}

		// Check if the target is tainted
		isTainted, taintedErr := e.targetCache.IsTainted(ctx, *target)
		if taintedErr != nil {
			logger.Errorf("Failed to check if target %s is tainted: %v", target.Label, taintedErr)
		}

		if isTainted {
			logger.Debugf("running target %s due to being tainted", target.Label)
		}

		// If either the inputs or the deps have changed we need to re-execute the target
		// depsCached is also true when there are no deps
		if depsCached && hasCacheHit && !isTainted {
			if e.loadOutputsMode == config.LoadOutputsMinimal {
				update(fmt.Sprintf("%s: cache hit. skipped loading outputs because load_outputs=minimal.", target.Label))
				logger.Debugf("%s: cache hit. skipped loading outputs because load_outputs=minimal", target.Label)
				return dag.CacheHit, nil
			}

			// TODO in the case of skip cache this log statement is a bit misleading
			update(fmt.Sprintf("%s: cache hit. loading outputs.", target.Label))
			loadingErr := e.loadCachedOutputs(ctx, target)
			if loadingErr != nil {
				// Don't return so that we instead break out and continue executing the target
				logger.Errorf("%s re-running due to output loading failure: %v", target.Label, loadingErr)
			} else {
				if target.IsTest() {
					executionTime := time.Since(startTime).Seconds()
					if testLogger := console.GetTestLogger(ctx); testLogger != nil {
						testLogger.LogTestPassedCached(logger, target.Label.String(), executionTime)
					} else {
						logger.Infof("%s %s (cached) in %.1fs", target.Label, color.New(color.FgGreen).Sprintf("PASSED"), executionTime)
					}
				}
				return dag.CacheHit, nil
			}
		}

		if !hasCacheHit {
			logger.Debugf("running target %s due to cache miss", target.Label)
		}
		if !depsCached {
			logger.Debugf("running target %s due to changed dependencis", target.Label)
		}
		if outputCheckErr != nil {
			logger.Debugf("running target %s due to output check error", target.Label)
		}

		if e.loadOutputsMode == config.LoadOutputsMinimal {
			update(fmt.Sprintf("%s: loading dependency outputs (load_outputs=minimal).", target.Label))
			if loadDepsErr := e.LoadDependencyOutputs(ctx, target, update); loadDepsErr != nil {
				return dag.CacheMiss, fmt.Errorf("failed to load dependency outputs for target %s: %w", target.Label, loadDepsErr)
			}
		}

		return e.executeTarget(ctx, target, binToolPaths, outputIdentifiers, update, isTainted)
	}
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
		update(fmt.Sprintf("%s: \"%s\"", target.Label, target.CommandEllipsis()))
		logger.Debugf("running target %s: %s", target.Label, target.CommandEllipsis())
		execStart := time.Now()
		err = executeTarget(ctx, target, binToolPaths, outputIdentifiers, e.streamLogsToggle.Enabled())
		e.addExecDuration(time.Since(execStart))

	} else {
		logger.Debugf("skipped target %s due to no command", target.Label)
	}
	executionTime := time.Since(startTime).Seconds()

	if err != nil {
		logger.Debugf("target execution returned error %s: %s", target.Label, err)
		if target.IsTest() && !errors.Is(err, context.Canceled) {
			// Test errors we want to log differently
			// Cancellations should just exit the execution
			if testLogger := console.GetTestLogger(ctx); testLogger != nil {
				testLogger.LogTestFailed(logger, target.Label.String(), executionTime)
			} else {
				logger.Infof("%s %s in %.1fs", target.Label, color.New(color.FgRed).Sprintf("FAILED"), executionTime)
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
			testLogger.LogTestPassed(logger, target.Label.String(), executionTime)
		} else {
			logger.Infof("%s %s in %.1fs", target.Label, color.New(color.FgGreen).Sprintf("PASSED"), executionTime)
		}
	}

	// If the target produced a bin output automatically mark it as executable
	err = markBinOutputExecutable(target)
	if err != nil {
		return dag.CacheMiss, err
	}

	// Write outputs to the cache:
	update(fmt.Sprintf("%s complete. writing outputs...", target.Label))
	err = e.registry.WriteOutputs(ctx, target)
	if err != nil {
		return dag.CacheMiss, fmt.Errorf("build completed but failed to write outputs to cache for target %s:\n%w", target.Label, err)
	}

	if isTainted {
		err = e.targetCache.RemoveTaint(ctx, *target)
		if err != nil {
			logger.Errorf("Failed to remove taint from target %s: %v", target.Label, err)
		}
	}

	return dag.CacheMiss, nil
}

func (e *Executor) loadCachedOutputs(
	ctx context.Context,
	target *model.Target,
) error {
	if target.SkipsCache() {
		return nil
	}

	err := e.registry.LoadOutputs(ctx, target)
	if err != nil {
		return fmt.Errorf("failed to read outputs from cache for target %s: %w", target.Label, err)
	}
	return nil
}

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
		err := e.loadCachedOutputs(ctx, dep)

		if err != nil || dep.SkipsCache() {
			logger.Debugf("%s: failed to load output for dependency %s (re-rerunning): err=%v no-cache=%t", target.Label, dep.Label, err, target.SkipsCache())
			// In this case we need to also recursively re-load the dependencies of the dependency
			if recursiveLoadErr := e.LoadDependencyOutputs(ctx, dep, update); recursiveLoadErr != nil {
				return recursiveLoadErr
			}

			binTools, binToolErr := e.getBinToolPaths(dep)
			if binToolErr != nil {
				return binToolErr
			}

			outputIdentifiers := e.getDependencyOutputIdentifiers(dep)

			update(fmt.Sprintf("%s: re-running dependency %s (load_outputs_mode=minimal).", target.Label, dep.Label))
			_, executionErr := e.executeTarget(ctx, dep, binTools, outputIdentifiers, update, false)
			if executionErr != nil {
				return executionErr
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
