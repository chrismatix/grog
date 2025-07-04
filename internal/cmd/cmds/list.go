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

var listOptions struct {
	targetType string
}

var ListCmd = &cobra.Command{
	Use:     "list",
	Aliases: []string{"ls"},
	Short:   "Lists targets by pattern.",
	Long:    `Lists targets that match the specified pattern. Can filter targets by type using the --target-type flag.`,
	Example: `  grog list                           # List all targets in the current package
  grog list //path/to/package:target    # List a specific target
  grog list //path/to/package/...       # List all targets in a package and subpackages
  grog list --target-type=test          # List only test targets`,
	Args:              cobra.ArbitraryArgs, // Optional argument for target pattern
	ValidArgsFunction: completions.AllTargetPatternCompletion,
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

		graph := loading.MustLoadGraphForQuery(ctx, logger)

		targetTypeFilter, err := selection.StringToTargetTypeSelection(listOptions.targetType)
		if err != nil {
			logger.Fatalf(err.Error())
		}
		selector := selection.New(targetPatterns, config.Global.Tags, config.Global.ExcludeTags, targetTypeFilter)
		selector.SelectTargets(graph)

		graph.LogSelectedNodes()
	},
}

func init() {
	ListCmd.Flags().StringVar(
		&listOptions.targetType,
		"target-type",
		"all",
		"Filter targets by type (all, test, no_test, bin_output)")
}
