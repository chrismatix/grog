package cmds

import (
	"github.com/spf13/cobra"
	"grog/internal/config"
	"grog/internal/label"
)

var TestCmd = &cobra.Command{
	Use:   "test",
	Short: "Loads the user configuration and executes test targets",
	Long:  `Loads the user configuration, checks which targets need to be rebuilt based on file hashes, builds the dependency graph, and executes targets.`,
	Args:  cobra.MaximumNArgs(1), // Optional argument for target pattern
	Run: func(cmd *cobra.Command, args []string) {
		ctx, logger := setupCommand()

		currentPackagePath, err := config.Global.GetCurrentPackage()
		if err != nil {
			logger.Fatalf("could not get current package: %v", err)
		}

		var targetPattern label.TargetPattern
		if len(args) > 0 {
			targetPattern, err = label.ParseTargetPattern(currentPackagePath, args[0])
			if err != nil {
				logger.Fatalf("could not parse target pattern: %v", err)
			}
		} else {
			targetPattern = label.GetMatchAllTargetPattern()
		}

		graph := mustLoadGraph(ctx, logger)

		runBuild(ctx, logger, targetPattern, graph, true)
	},
}
