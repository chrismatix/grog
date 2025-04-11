package execution

import (
	"context"
	"errors"
	"fmt"
	tea "github.com/charmbracelet/bubbletea"
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

func Execute(
	ctx context.Context,
	graph *dag.DirectedTargetGraph,
	failFast bool,
) (error, dag.CompletionMap) {
	numWorkers := viper.GetInt("num_workers")
	logger := console.GetLogger(ctx)

	p, msgCh := console.StartTaskUI(ctx)
	defer func(p *tea.Program) {
		err := p.ReleaseTerminal()
		if err != nil {
			logger.Errorf("error releasing terminal: %v", err)
		}
	}(p)
	defer p.Quit()

	workerPool := worker.NewPool(numWorkers, msgCh)
	workerPool.StartWorkers(ctx)

	// walkCallback will be called at max parallelism by the graph walker
	walkCallback := func(ctx context.Context, target model.Target) error {
		// taskFunc will be run in the worker pool
		taskFunc := GetTaskFunc(target)

		// awaits execution of taskFunc
		return workerPool.Run(taskFunc)
	}

	walker := dag.NewWalker(graph, walkCallback, failFast)
	return walker.Walk(ctx)
}

func GetTaskFunc(target model.Target) worker.TaskFunc {
	return func(update worker.StatusFunc, log worker.LogFunc) error {
		executionPath := config.GetPathAbsoluteToWorkspaceRoot(target.Label.Package)

		cmd := exec.Command("sh", "-c", target.Command)
		cmd.Dir = executionPath
		update(fmt.Sprintf("%s: \"%s\"", target.Label, target.CommandEllipsis()))
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
}
