package execution

import (
	"context"
	"errors"
	"fmt"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/fatih/color"
	"grog/internal/config"
	"grog/internal/console"
	"grog/internal/dag"
	"grog/internal/hashing"
	"grog/internal/label"
	"grog/internal/model"
	"grog/internal/output"
	"grog/internal/worker"
	"os"
	"path/filepath"
	"time"
)

type CommandError struct {
	TargetLabel label.TargetLabel
	ExitCode    int
	Output      string
}

func (e *CommandError) Error() string {
	return fmt.Sprintf("target %s failed with exit code %d: %s", e.TargetLabel, e.ExitCode, e.Output)
}

// Execute executes the targets in the given graph and returns the completion map
func Execute(
	ctx context.Context,
	registry *output.Registry,
	graph *dag.DirectedTargetGraph,
	failFast bool,
	streamLogs bool,
) (dag.CompletionMap, error) {
	numWorkers := config.Global.NumWorkers
	stdLogger := console.GetLogger(ctx)

	program, msgCh := console.StartTaskUI(ctx)
	defer func(p *tea.Program) {
		err := p.ReleaseTerminal()
		if err != nil {
			stdLogger.Errorf("error releasing terminal: %v", err)
		}
	}(program)
	defer program.Quit()

	// Attach the tea logger to the context
	ctx = console.WithTeaLogger(ctx, program)

	// Get selected vertices and create a TestLogger for them
	selectedVertices := graph.GetSelectedVertices()
	selectedTargetCount := len(selectedVertices)

	// Create a list of target labels for the TestLogger
	targetLabels := make([]string, selectedTargetCount)
	for i, target := range selectedVertices {
		targetLabels[i] = target.Label.String()
	}

	// Create a TestLogger with the target labels
	testLogger := console.NewTestLogger(targetLabels, 0) // 0 means use default terminal width

	// Add the TestLogger to the context
	ctx = context.WithValue(ctx, console.TestLoggerKey{}, testLogger)

	workerPool := worker.NewTaskWorkerPool[dag.CacheResult](stdLogger, numWorkers, msgCh, selectedTargetCount)
	workerPool.StartWorkers(ctx)
	defer workerPool.Shutdown()

	// walkCallback will be called at max parallelism by the graph walker
	walkCallback := func(ctx context.Context, target *model.Target, depsCached bool) (dag.CacheResult, error) {
		// get any possible bin tools that the target may use
		binTools, err := getBinToolPaths(graph, target)
		if err != nil {
			return dag.CacheMiss, err
		}

		dependencies := graph.GetDependencies(target)

		dependencyHashes := make([]string, len(dependencies))
		for index, dep := range dependencies {
			dependencyHashes[index] = dep.ChangeHash
		}

		// Set the change hash that will determine if the target changed
		changeHash, err := hashing.GetTargetChangeHash(*target, dependencyHashes)
		if err != nil {
			return dag.CacheMiss, err
		}
		target.ChangeHash = changeHash

		// taskFunc will be run in the worker pool
		taskFunc := GetTaskFunc(ctx, registry, target, binTools, depsCached, streamLogs)
		return workerPool.Run(taskFunc)
	}

	walker := dag.NewWalker(graph, walkCallback, failFast)
	completionMap, err := walker.Walk(ctx)
	return completionMap, err
}

// getBinToolPaths From all the direct dependencies of a target, get their bin_output if defined
func getBinToolPaths(graph *dag.DirectedTargetGraph, target *model.Target) (BinToolMap, error) {
	deps := graph.GetDependencies(target)

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

func GetTaskFunc(
	ctx context.Context,
	registry *output.Registry,
	target *model.Target,
	binToolPaths BinToolMap,
	depsCached bool,
	streamLogs bool,
) worker.TaskFunc[dag.CacheResult] {
	return func(update worker.StatusFunc) (dag.CacheResult, error) {
		startTime := time.Now()

		update(fmt.Sprintf("%s: checking cache.", target.Label))
		hasCacheHit, err := registry.HasCacheHit(ctx, *target)
		if err != nil {
			return dag.CacheMiss, err
		}
		target.HasCacheHit = hasCacheHit

		logger := console.GetLogger(ctx)

		outputCheckErr := runOutputChecks(ctx, target, binToolPaths)
		if outputCheckErr != nil {
			logger.Debugf("running target due to output check error: %v", outputCheckErr)
		}

		// If either the inputs or the deps have changed we need to re-execute the target
		// depsCached is also true when there are no deps
		if depsCached && outputCheckErr == nil {
			if target.HasOutputChecksOnly() {
				// Special type of target that only has output checks but no in- and outputs
				// In this case the output checks (and the dependencies) are the only thing affecting re-running
				logger.Debugf("skipping target %s due to passed output checks", target.Label)
				return dag.CacheSkip, nil
			}

			if hasCacheHit {
				update(fmt.Sprintf("%s: cache hit. loading outputs.", target.Label))
				loadingErr := loadCachedOutputs(ctx, registry, target)
				if loadingErr != nil {
					// Don't return so that we instead break out and continue executing the target
					logger.Errorf("failed to load outputs from cache for target %s: %v", target.Label, loadingErr)
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

			} else {
				logger.Debugf("running target %s due to cache miss", target.Label)
			}
		}

		if !depsCached {
			logger.Debugf("running target %s due to changed dependencis", target.Label)
		}
		if outputCheckErr != nil {
			logger.Debugf("running target %s due to output check error", target.Label)
		}

		if target.Command != "" {
			update(fmt.Sprintf("%s: \"%s\"", target.Label, target.CommandEllipsis()))
			logger.Debugf("running target %s: %s", target.Label, target.CommandEllipsis())
			err = executeTarget(ctx, target, binToolPaths, streamLogs)
		} else {
			logger.Debugf("skpped target %s due to no command", target.Label)
		}
		executionTime := time.Since(startTime).Seconds()

		if err != nil {
			if target.IsTest() && !errors.Is(err, context.Canceled) {
				if testLogger := console.GetTestLogger(ctx); testLogger != nil {
					testLogger.LogTestFailed(logger, target.Label.String(), executionTime)
				} else {
					logger.Infof("%s %s in %.1fs", target.Label, color.New(color.FgRed).Sprintf("FAILED"), executionTime)
				}
			}
			return dag.CacheMiss, err
		}

		// Run output checks again to see if they match now
		if outputCheckErr = runOutputChecks(ctx, target, binToolPaths); outputCheckErr != nil {
			return dag.CacheMiss, outputCheckErr
		}

		if target.IsTest() {
			if testLogger := console.GetTestLogger(ctx); testLogger != nil {
				testLogger.LogTestPassed(logger, target.Label.String(), executionTime)
			} else {
				logger.Infof("%s %s in %.1fs", target.Label, color.New(color.FgGreen).Sprintf("PASSED"), executionTime)
			}
		}

		// If the target produced a bin output automatically mark it executable
		err = markBinOutputExecutable(target)
		if err != nil {
			return dag.CacheMiss, err
		}

		// Write outputs to the cache:
		update(fmt.Sprintf("%s complete. writing outputs...", target.Label))
		err = registry.WriteOutputs(ctx, *target)
		if err != nil {
			return dag.CacheMiss, fmt.Errorf("build completed but failed to write outputs to cache for target %s:\n%w", target.Label, err)
		}
		return dag.CacheMiss, nil
	}
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

func loadCachedOutputs(ctx context.Context, registry *output.Registry, target *model.Target) error {
	if target.SkipsCache() {
		return nil
	}

	err := registry.LoadOutputs(ctx, *target)
	if err != nil {
		return fmt.Errorf("failed to read outputs from cache for target %s: %w", target.Label, err)
	}
	return nil
}
