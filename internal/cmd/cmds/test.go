package cmds

import (
	"github.com/spf13/cobra"
	"grog/internal/completions"
	"grog/internal/config"
	"grog/internal/console"
	"grog/internal/execution"
	"grog/internal/label"
	"grog/internal/loading"
	"grog/internal/selection"
)

var TestCmd = &cobra.Command{
	Use:   "test",
	Short: "Loads the user configuration and executes test targets.",
	Long: `Loads the user configuration, checks which targets need to be rebuilt based on file hashes, builds the dependency graph, and executes test targets.

Use "--" to separate the list of targets from additional arguments passed to the target commands.`,
	Example: `  grog test                                          # Run all tests in the current package and subpackages
  grog test //path/to/package:test                   # Run a specific test
  grog test //path/to/package/...                    # Run all tests in a package and subpackages
  grog test //path/to/package:test -- -k test_foo    # Pass extra arguments to the test command`,
	Args:              cobra.ArbitraryArgs, // Optional argument for target pattern
	ValidArgsFunction: completions.TestTargetPatternCompletion,
	Run: func(cmd *cobra.Command, args []string) {
		ctx, logger := console.SetupCommand()

		// Split target patterns from extra args at "--". Unlike grog run,
		// grog test allows omitting the target pattern (defaults to current
		// package), so "grog test -- -x" is valid.
		var targetArgs, extraArgs []string
		if dash := cmd.ArgsLenAtDash(); dash == 0 {
			// "--" is the first argument: no targets, everything is extra args
			extraArgs = args
		} else if dash > 0 {
			targetArgs = args[:dash]
			extraArgs = args[dash:]
		} else {
			// No "--" found: all args are target patterns
			targetArgs = args
		}

		currentPackagePath, err := config.Global.GetCurrentPackage()
		if err != nil {
			logger.Fatalf("could not get current package: %v", err)
		}

		targetPatterns, err := label.ParsePatternsOrMatchCurrentPackageAndSubpackages(currentPackagePath, targetArgs)
		if err != nil {
			logger.Fatalf("could not parse target pattern: %v", err)
		}

		if len(extraArgs) > 0 {
			ctx = execution.WithExtraArgs(ctx, extraArgs)
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
