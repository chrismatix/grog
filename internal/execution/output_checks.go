package execution

import (
	"context"
	"fmt"
	"grog/internal/model"
	"strings"
)

func runOutputChecks(ctx context.Context, target *model.Target, binToolPaths BinToolMap, outputIdentifiers OutputIdentifierMap) error {
	for _, check := range target.OutputChecks {
		output, err := runTargetCommand(ctx, target, binToolPaths, outputIdentifiers, check.Command, false)
		if err != nil {
			return fmt.Errorf("output check failed for target %s: %w\ncommand %s",
				target.Label, err, check.Command)
		}

		expected := strings.TrimSpace(check.ExpectedOutput)
		actual := strings.TrimSpace(string(output))
		if check.ExpectedOutput != "" && expected != actual {
			return fmt.Errorf("output check failed: expected '%s', got '%s'\ncommand %s",
				expected, actual, check.Command)
		}
	}

	return nil
}
