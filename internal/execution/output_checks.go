package execution

import (
	"context"
	"fmt"
	"grog/internal/model"
	"strings"
)

func runOutputChecks(ctx context.Context, target *model.Target, binToolPaths BinToolMap) error {
	for _, check := range target.OutputChecks {
		output, err := runTargetCommand(ctx, target, binToolPaths, check.Command, false)
		if err != nil {
			return fmt.Errorf("output check failed for target %s: %w\ncommand %s",
				target.Label, err, check.Command)
		}

		if check.ExpectedOutput != "" && strings.TrimSpace(check.ExpectedOutput) != strings.TrimSpace(string(output)) {
			return fmt.Errorf("output check failed: expected '%s', got '%s'\ncommand %s",
				check.ExpectedOutput, output, check.Command)
		}
	}

	return nil
}
