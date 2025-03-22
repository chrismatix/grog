package execution

import (
	"context"
	"errors"
	"fmt"
	"github.com/spf13/viper"
	"grog/internal/config"
	"grog/internal/console"
	"grog/internal/dag"
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

func Execute(ctx context.Context, graph *dag.DirectedTargetGraph, failFast bool) (error, dag.CompletionMap) {
	numWorkers := viper.GetInt("num_workers")

	p, msgCh := console.StartTaskUI(ctx)
	defer p.ReleaseTerminal()
	defer p.Quit()

	workerPool := worker.NewPool(numWorkers, msgCh)

	// walkCallback will be called at max parallelism by the graph walker
	walkCallback := func(ctx context.Context, target model.Target) error {
		executionPath := config.GetPatAbsoluteToWorkspaceRoot(target.Label.Package)

		// taskFunc will be run in the worker pool
		taskFunc := func(update worker.StatusFunc, log worker.LogFunc) error {
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

		// awaits execution of taskFunc
		return workerPool.Run(taskFunc)
	}

	walker := dag.NewWalker(graph, walkCallback, failFast)
	return walker.Walk(ctx)
}
