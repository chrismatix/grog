package cmds

import (
	"github.com/spf13/cobra"
	"grog/internal/completions"
	"grog/internal/config"
	"grog/internal/console"
	"grog/internal/label"
	"grog/internal/loading"
	"grog/internal/selection"
)

var TestCmd = &cobra.Command{
	Use:   "test",
	Short: "Loads the user configuration and executes test targets.",
	Long:  `Loads the user configuration, checks which targets need to be rebuilt based on file hashes, builds the dependency graph, and executes test targets.`,
	Example: `  grog test                      # Run all tests in the current package
  grog test //path/to/package:test  # Run a specific test
  grog test //path/to/package/...   # Run all tests in a package and subpackages`,
	Args:              cobra.ArbitraryArgs, // Optional argument for target pattern
	ValidArgsFunction: completions.TestTargetPatternCompletion,
	Run: func(cmd *cobra.Command, args []string) {
		ctx, logger := console.SetupCommand()

		currentPackagePath, err := config.Global.GetCurrentPackage()
		if err != nil {
			logger.Fatalf("could not get current package: %v", err)
		}

		targetPatterns, err := label.ParsePatternsOrMatchAll(currentPackagePath, args)
		if err != nil {
			logger.Fatalf("could not parse target pattern: %v", err)
		}

		graph := loading.MustLoadGraphForBuild(ctx, logger)

		RunBuild(
			ctx,
			logger,
			targetPatterns,
			graph,
			selection.TestOnly,
			config.Global.StreamLogs,
			config.Global.GetLoadOutputsMode(),
		)
	},
}

func AddTestCmd(rootCmd *cobra.Command) {
	rootCmd.AddCommand(TestCmd)
}
