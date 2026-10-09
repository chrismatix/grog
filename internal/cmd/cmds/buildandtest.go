package cmds

import (
	"github.com/spf13/cobra"

	"github.com/chrismatix/grog/internal/completions"
	"github.com/chrismatix/grog/internal/config"
	"github.com/chrismatix/grog/internal/console"
	"github.com/chrismatix/grog/internal/label"
	"github.com/chrismatix/grog/internal/loading"
	"github.com/chrismatix/grog/internal/selection"
)

var BuildAndTestCmd = &cobra.Command{
	Use:     "build-and-test",
	Aliases: []string{"bat"},
	Short:   "Loads the user configuration and executes build and test targets.",
	Long:    `Loads the user configuration, checks which targets need to be rebuilt based on file hashes, builds the dependency graph, and executes both build and test targets.`,
	Example: `  grog build-and-test                      # Build all targets and run all tests in the current package and subpackages
  grog build-and-test //path/to/package:target  # Build or test a specific target
  grog build-and-test //path/to/package/...     # Build all targets and run all tests in a package and subpackages`,
	Args:              cobra.ArbitraryArgs,
	ValidArgsFunction: completions.AllTargetPatternCompletion,
	Run: func(cmd *cobra.Command, args []string) {
		ctx, logger := console.SetupCommand()

		currentPackagePath, err := config.Global.GetCurrentPackage()
		if err != nil {
			logger.Fatalf("could not get current package: %v", err)
		}

		targetPatterns, err := label.ParsePatternsOrMatchCurrentPackageAndSubpackages(currentPackagePath, args)
		if err != nil {
			logger.Fatalf("could not parse target pattern: %v", err)
		}

		graph := loading.MustLoadGraphForBuild(ctx, logger, dependencyInferrer())

		RunBuild(
			ctx,
			logger,
			targetPatterns,
			graph,
			selection.AllTargets,
			config.Global.StreamLogs,
			config.Global.GetLoadOutputsMode(),
		)
	},
}

func AddBuildAndTestCmd(rootCmd *cobra.Command) {
	rootCmd.AddCommand(BuildAndTestCmd)
}
