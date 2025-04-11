package execution

import (
	"context"
	"errors"
	"fmt"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/spf13/viper"
	"grog/internal/caching"
	"grog/internal/config"
	"grog/internal/console"
	"grog/internal/dag"
	"grog/internal/hashing"
	"grog/internal/label"
	"grog/internal/model"
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

func Execute(
	ctx context.Context,
	cache caching.Cache,
	graph *dag.DirectedTargetGraph,
	failFast bool,
) (error, dag.CompletionMap) {
	numWorkers := viper.GetInt("num_workers")
	logger := console.GetLogger(ctx)

	program, msgCh := console.StartTaskUI(ctx)
	defer func(p *tea.Program) {
		err := p.ReleaseTerminal()
		if err != nil {
			logger.Errorf("error releasing terminal: %v", err)
		}
	}(program)
	defer program.Quit()

	workerPool := worker.NewPool(numWorkers, msgCh)
	workerPool.StartWorkers(ctx)

	targetCache := caching.NewTargetCache(cache)

	// walkCallback will be called at max parallelism by the graph walker
	walkCallback := func(ctx context.Context, target *model.Target) error {
		// taskFunc will be run in the worker pool
		taskFunc := GetTaskFunc(targetCache, target)

		// awaits execution of taskFunc
		return workerPool.Run(taskFunc)
	}

	walker := dag.NewWalker(graph, walkCallback, failFast)
	return walker.Walk(ctx)
}

func GetTaskFunc(targetCache *caching.TargetCache, target *model.Target) worker.TaskFunc {
	return func(update worker.StatusFunc, log worker.LogFunc) error {
		changeHash, err := hashing.GetTargetChangeHash(*target)
		if err != nil {
			return err
		}
		target.ChangeHash = changeHash

		hasCacheHit := targetCache.HasCacheHit(*target)
		target.HasCacheHit = hasCacheHit

		if hasCacheHit {
			if len(target.Outputs) > 0 {
				update(fmt.Sprintf("%s: cache hit (%s). fetching...", target.Label, targetCache.GetCache().TypeName()))
				return downloadCachedOutputs(targetCache, target)
			} else {
				update(fmt.Sprintf("%s: cache hit (%s).", target.Label, targetCache.GetCache().TypeName()))
				return nil
			}
		}

		update(fmt.Sprintf("%s: \"%s\"", target.Label, target.CommandEllipsis()))
		err = executeTarget(target)
		if err != nil {
			return err
		}

		// Write outputs to the cache:
		update(fmt.Sprintf("%s: cache hit (%s). fetching ", target.Label, targetCache.GetCache().TypeName()))
		err = targetCache.WriteOutputs(*target)
		if err != nil {
			return fmt.Errorf("build completed but failed to write outputs to cache for target %s:\n%w", target.Label, err)
		}
		return nil
	}
}

func downloadCachedOutputs(targetCache *caching.TargetCache, target *model.Target) error {
	err := targetCache.LoadOutputs(*target)
	if err != nil {
		return fmt.Errorf("build completed but failed to read outputs from cache for target %s: %w", target.Label, err)
	}
	return nil
}

func executeTarget(target *model.Target) error {
	executionPath := config.GetPathAbsoluteToWorkspaceRoot(target.Label.Package)

	cmd := exec.Command("sh", "-c", target.Command)
	cmd.Dir = executionPath

	output, err := cmd.CombinedOutput()

	if err != nil {
		var exitError *exec.ExitError
		if errors.As(err, &exitError) {
			return &CommandError{
				TargetLabel: target.Label,
				ExitCode:    exitError.ExitCode(),
				Output:      string(output),
			}
		}
		return fmt.Errorf("target %s failed: %w - output: %s", target.Label, err, string(output))
	}
	return nil
}
