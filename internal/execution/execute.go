package execution

import (
	"context"
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

	selectedTargetCount := len(graph.GetSelectedVertices())
	workerPool := worker.NewTaskWorkerPool[dag.CacheResult](numWorkers, msgCh, selectedTargetCount)
	workerPool.StartWorkers(ctx)
	defer workerPool.Shutdown()

	// walkCallback will be called at max parallelism by the graph walker
	walkCallback := func(ctx context.Context, target *model.Target, depsCached bool) (dag.CacheResult, error) {
		// get any possible bin tools that the target may use
		binTools, err := getBinToolPaths(graph, target)
		if err != nil {
			return dag.CacheMiss, err
		}

		// taskFunc will be run in the worker pool
		taskFunc := GetTaskFunc(ctx, registry, target, binTools, depsCached)
		// awaits execution of taskFunc
		return workerPool.Run(taskFunc)
	}

	walker := dag.NewWalker(graph, walkCallback, failFast)
	completionMap, err := walker.Walk(ctx)
	return completionMap, err
}

// getBinToolPaths From all the direct dependencies of a target, get their bin_output if defined
func getBinToolPaths(graph *dag.DirectedTargetGraph, target *model.Target) (BinToolMap, error) {
	deps, err := graph.GetInEdges(target)
	if err != nil {
		return nil, err
	}

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
) worker.TaskFunc[dag.CacheResult] {
	// taskFunc will run in the worker pool and return a bool indicating whether the target was cached
	return func(update worker.StatusFunc) (dag.CacheResult, error) {
		startTime := time.Now()
		changeHash, err := hashing.GetTargetChangeHash(*target)
		if err != nil {
			return dag.CacheMiss, err
		}
		target.ChangeHash = changeHash

		update(fmt.Sprintf("%s: checking cache.", target.Label))
		hasCacheHit, err := registry.HasCacheHit(ctx, *target)
		if err != nil {
			return dag.CacheMiss, err
		}
		target.HasCacheHit = hasCacheHit

		logger := console.GetLogger(ctx)

		// If either the inputs or the deps have changed we need to re-execute the target
		// depsCached is also true when there are no deps
		if hasCacheHit && depsCached && !target.SkipsCache() {
			update(fmt.Sprintf("%s: cache hit. loading outputs.", target.Label))
			loadingErr := loadCachedOutputs(ctx, registry, target)
			if loadingErr != nil {
				// Don't return so that we instead break out and continue executing the target
				logger.Errorf("failed to load outputs from cache for target %s: %v", target.Label, loadingErr)
			} else {
				if target.IsTest() {
					executionTime := time.Since(startTime).Seconds()
					logger.Infof("%s %s (cached) in %.1fs", target.Label, color.New(color.FgGreen).Sprintf("PASSED"), executionTime)
				}
				return dag.CacheHit, nil
			}
		}

		update(fmt.Sprintf("%s: \"%s\"", target.Label, target.CommandEllipsis()))
		err = executeTarget(ctx, target, binToolPaths)
		executionTime := time.Since(startTime).Seconds()
		if err != nil {
			logger.Infof("%s %s in %.1fs", target.Label, color.New(color.FgRed).Sprintf("FAILED"), executionTime)
			return dag.CacheMiss, err
		}

		// If the target was a test log the result
		if target.IsTest() {
			logger.Infof("%s %s in %.1fs", target.Label, color.New(color.FgGreen).Sprintf("PASSED"), executionTime)
		}

		// If the target produced a bin output automatically mark it executable
		err = markBinOutputExecutable(target)
		if err != nil {
			return dag.CacheMiss, err
		}

		if target.SkipsCache() {
			return dag.CacheSkip, nil
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
	err := registry.LoadOutputs(ctx, *target)
	if err != nil {
		return fmt.Errorf("failed to read outputs from cache for target %s: %w", target.Label, err)
	}
	return nil
}
