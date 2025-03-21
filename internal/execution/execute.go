package execution

import (
	"context"
	"errors"
	"fmt"
	"grog/internal/config"
	"grog/internal/dag"
	"grog/internal/label"
	"grog/internal/model"
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
	walkCallback := func(ctx context.Context, target model.Target) error {
		executionPath := config.GetPatAbsoluteToWorkspaceRoot(target.Label.Package)

		cmd := exec.Command("sh", target.Command)
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

	walker := dag.NewWalker(graph, walkCallback, failFast)
	return walker.Walk(ctx)
}
