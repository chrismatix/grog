package cmds

import (
	"github.com/spf13/cobra"
	"grog/internal/analysis"
	"grog/internal/config"
	"grog/internal/label"
	"grog/internal/loading"
	"grog/internal/model"
)

var BuildCmd = &cobra.Command{
	Use:   "build",
	Short: "Loads the build configuration and executes targets",
	Long:  `Loads the build configuration, checks which targets need to be rebuilt based on file hashes, builds the dependency graph, and executes targets.`,
	Args:  cobra.MaximumNArgs(1), // Optional argument for target pattern
	Run: func(cmd *cobra.Command, args []string) {
		logger := config.GetLogger()

		packages, err := loading.LoadPackages()
		if err != nil {
			logger.Fatalf(
				"could not load packages: %v",
				err)
		}

		numPackages := len(packages)
		targets, err := model.TargetMapFromPackages(packages)
		if err != nil {
			logger.Fatalf("could not create target map: %v", err)
		}

		graph, err := analysis.BuildGraphAndAnalyze(targets)
		if err != nil {
			logger.Fatalf("could not build graph: %v", err)
		}

		if len(args) > 0 {
			targetPattern, err := label.ParseTargetPattern(args[0])
			if err != nil {
				logger.Fatalf("could not parse target pattern: %v", err)
			}

			selectedCount := graph.SelectTargets(targetPattern)
			logger.Infof("Selected %d targets (%d packages loaded, %d targets configured).", selectedCount, numPackages, len(targets))
		} else {
			// No target pattern: build all targets
			graph.SelectAllTargets()
			logger.Infof("Selected all targets (%d packages loaded, %d targets configured).", numPackages, len(targets))
		}

	},
}
