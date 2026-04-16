package traces

import (
	"fmt"
	"time"

	"github.com/spf13/cobra"

	"grog/internal/console"
)

var pruneOlderThan string

var pruneCmd = &cobra.Command{
	Use:   "prune",
	Short: "Delete traces older than a specified duration.",
	Example: `  grog traces prune --older-than 30d
  grog traces prune --older-than 7d`,
	Args: cobra.NoArgs,
	Run: func(cmd *cobra.Command, args []string) {
		ctx, logger := console.SetupCommand()
		store := getStore(ctx, logger)
		defer store.Close()

		duration, err := parseDuration(pruneOlderThan)
		if err != nil {
			logger.Fatalf("invalid --older-than value: %v", err)
		}

		cutoff := time.Now().Add(-duration)
		pruned, pruneErr := store.Prune(ctx, cutoff)
		if pruneErr != nil {
			logger.Fatalf("prune failed: %v", pruneErr)
		}

		logger.Infof("Pruned %d traces older than %s.", pruned, pruneOlderThan)
	},
}

func registerPruneCmd() {
	pruneCmd.Flags().StringVar(&pruneOlderThan, "older-than", "30d", "Delete traces older than this duration (e.g. 30d, 72h)")
	Cmd.AddCommand(pruneCmd)
}

func parseDuration(s string) (time.Duration, error) {
	if len(s) < 2 {
		return 0, fmt.Errorf("duration too short: %q", s)
	}

	unit := s[len(s)-1]
	valueStr := s[:len(s)-1]
	var value int
	if _, err := fmt.Sscanf(valueStr, "%d", &value); err != nil {
		return 0, fmt.Errorf("invalid duration value: %q", s)
	}

	switch unit {
	case 'd':
		return time.Duration(value) * 24 * time.Hour, nil
	case 'h':
		return time.Duration(value) * time.Hour, nil
	default:
		return 0, fmt.Errorf("unknown unit %q in %q (use d for days, h for hours)", string(unit), s)
	}
}
