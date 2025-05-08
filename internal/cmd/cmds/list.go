package cmds

import (
	"github.com/spf13/cobra"
	"grog/internal/config"
	"grog/internal/label"
	"grog/internal/loading"
	"grog/internal/selection"
)

var ListCmd = &cobra.Command{
	Use:   "list",
	Short: "Lists targets by pattern.",
	Args:  cobra.ArbitraryArgs, // Optional argument for target pattern
	Run: func(cmd *cobra.Command, args []string) {
		ctx, logger := setupCommand()

		currentPackagePath, err := config.Global.GetCurrentPackage()
		if err != nil {
			logger.Fatalf("could not get current package: %v", err)
		}

		targetPatterns, err := label.ParsePatternsOrMatchAll(currentPackagePath, args)
		if err != nil {
			logger.Fatalf("could not parse target pattern: %v", err)
		}

		graph := loading.MustLoadGraphForQuery(ctx, logger)
		selector := selection.New(targetPatterns, config.Global.Tags, selection.AllTargets)
		if err := selector.SelectTargets(graph); err != nil {
			logger.Fatalf("target selection failed: %v", err)
		}

		graph.LogSelectedVertices()
	},
}
