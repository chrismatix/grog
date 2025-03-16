package cmds

import (
	"github.com/spf13/cobra"
	"grog/pkg/config"
	"grog/pkg/label"
	"grog/pkg/loading"
	"grog/pkg/model"
)

var BuildCmd = &cobra.Command{
	Use:   "build",
	Short: "Loads the build configuration and executes targets",
	Long:  `Loads the build configuration, checks which targets need to be rebuilt based on file hashes, builds the dependency graph, and executes targets.`,
	Args:  cobra.MaximumNArgs(1), // Optional argument for target pattern
	Run: func(cmd *cobra.Command, args []string) {
		logger := config.GetLogger()

		packages, err := loading.LoadPackages(logger)
		if err != nil {
			logger.Fatalf(
				"could not load packages: %v",
				err)
		}

		targetPattern := label.TargetPattern{}
		hasTargetPattern := false
		if len(args) > 0 {
			hasTargetPattern = true
			targetPattern, err = label.ParseTargetPattern(args[0])
			if err != nil {
				logger.Fatalf(
					"could not parse target pattern: %v",
					err)
			}
		}

		numTargets := 0
		targets := []model.Target{}
		for _, pkg := range packages {
			for _, target := range pkg.Targets {
				if hasTargetPattern {
					if targetPattern.Matches(target.Label) {
						targets = append(targets, target)
					}
				} else {
					// No target pattern: add all targets
					targets = append(targets, target)
				}
			}
			numTargets += len(pkg.Targets)
		}

		numPackages := len(packages)

		logger.Infof("Analyzed %d targets (%d packages loaded, %d targets configured).", numTargets, numPackages, numTargets)
	},
}
