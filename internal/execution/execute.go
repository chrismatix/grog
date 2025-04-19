package execution

import (
	"context"
	"errors"
	"fmt"
	tea "github.com/charmbracelet/bubbletea"
	"grog/internal/config"
	"grog/internal/console"
	"grog/internal/dag"
	"grog/internal/hashing"
	"grog/internal/label"
	"grog/internal/model"
	"grog/internal/output"
	"grog/internal/worker"
	"os/exec"
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
	ctx = console.WithTeaLogging(ctx, program)

	selectedTargetCount := len(graph.GetSelectedVertices())
	workerPool := worker.NewPool[bool](numWorkers, msgCh, selectedTargetCount)
	workerPool.StartWorkers(ctx)

	// walkCallback will be called at max parallelism by the graph walker
	walkCallback := func(ctx context.Context, target *model.Target, depsCached bool) (bool, error) {
		// taskFunc will be run in the worker pool
		taskFunc := GetTaskFunc(ctx, registry, target, depsCached)
		// awaits execution of taskFunc
		return workerPool.Run(taskFunc)
	}

	walker := dag.NewWalker(graph, walkCallback, failFast)
	completionMap, err := walker.Walk(ctx)
	return completionMap, err
}

func GetTaskFunc(
	ctx context.Context,
	registry *output.Registry,
	target *model.Target,
	depsCached bool,
) worker.TaskFunc[bool] {
	// taskFunc will run in the worker pool and return a bool indicating whether the target was cached
	return func(update worker.StatusFunc) (bool, error) {
		changeHash, err := hashing.GetTargetChangeHash(*target)
		if err != nil {
			return false, err
		}
		target.ChangeHash = changeHash

		hasCacheHit, err := registry.HasCacheHit(ctx, *target)
		if err != nil {
			return false, err
		}
		target.HasCacheHit = hasCacheHit

		// If either the inputs or the deps have changed we need to re-execute the target
		// depsCached is also true when there are no deps
		if hasCacheHit && depsCached {
			if len(target.Outputs) > 0 {
				update(fmt.Sprintf("%s: cache hit. fetching outputs...", target.Label))
				loadingErr := loadCachedOutputs(ctx, registry, target)
				if loadingErr != nil {
					// Don't return so that we instead break out and continue executing the target
					console.GetLogger(ctx).Errorf("failed to load outputs from cache for target %s: %v", target.Label, loadingErr)
				} else {
					return true, nil
				}
			} else {
				update(fmt.Sprintf("%s: cache hit.", target.Label))
				return true, nil
			}
		}

		update(fmt.Sprintf("%s: \"%s\"", target.Label, target.CommandEllipsis()))
		err = executeTarget(ctx, target)
		if err != nil {
			return false, err
		}

		// Write outputs to the cache:
		update(fmt.Sprintf("%s complete. writing outputs...", target.Label))
		err = registry.WriteOutputs(ctx, *target)
		if err != nil {
			return false, fmt.Errorf("build completed but failed to write outputs to cache for target %s:\n%w", target.Label, err)
		}
		return false, nil
	}
}

func loadCachedOutputs(ctx context.Context, registry *output.Registry, target *model.Target) error {
	err := registry.LoadOutputs(ctx, *target)
	if err != nil {
		return fmt.Errorf("failed to read outputs from cache for target %s: %w", target.Label, err)
	}
	return nil
}

func executeTarget(ctx context.Context, target *model.Target) error {
	executionPath := config.GetPathAbsoluteToWorkspaceRoot(target.Label.Package)

	cmd := exec.CommandContext(ctx, "sh", "-c", target.Command)
	cmd.Dir = executionPath

	cmdOut, err := cmd.CombinedOutput()

	if err != nil {
		var exitError *exec.ExitError
		if errors.As(err, &exitError) {
			return &CommandError{
				TargetLabel: target.Label,
				ExitCode:    exitError.ExitCode(),
				Output:      string(cmdOut),
			}
		}
		return fmt.Errorf("target %s failed: %w - output: %s", target.Label, err, string(cmdOut))
	}
	return nil
}
