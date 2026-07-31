package cmds

import (
	"sort"

	"grog/internal/config"
	"grog/internal/console"
	"grog/internal/label"
	"grog/internal/loading"
	"grog/internal/selection"

	"github.com/spf13/cobra"
)

var verifyOptions = struct {
	since string
}{
	since: "HEAD",
}

var verifyCmd = &cobra.Command{
	Use:     "verify",
	Aliases: []string{"v"},
	Short:   "Builds and tests targets affected by repository changes.",
	Long:    `Finds targets affected by changes since a Git ref or Jujutsu revision, includes their transitive dependents, and builds and tests the resulting graph.`,
	Example: `  grog verify               # Verify uncommitted changes
  grog v                    # Short alias
  grog verify --since=main  # Verify all changes since main`,
	Args: cobra.NoArgs,
	Run: func(command *cobra.Command, arguments []string) {
		commandContext, logger := console.SetupCommand()

		changedFiles, err := getChangedFiles(verifyOptions.since)
		if err != nil {
			logger.Fatalf("failed to get changed files: %v", err)
		}
		if len(changedFiles) == 0 {
			logger.Info("No changes to verify.")
			return
		}

		graph := loading.MustLoadGraphForBuild(commandContext, logger)
		affectedTargets := findTargetsAffectedByFiles(graph, changedFiles, true)
		if len(affectedTargets) == 0 {
			logger.Info("No Grog targets are affected.")
			return
		}

		sort.Slice(affectedTargets, func(firstIndex int, secondIndex int) bool {
			return affectedTargets[firstIndex].Label.String() < affectedTargets[secondIndex].Label.String()
		})
		targetPatterns := make([]label.TargetPattern, 0, len(affectedTargets))
		for _, target := range affectedTargets {
			targetPatterns = append(targetPatterns, label.TargetPatternFromLabel(target.Label))
		}

		RunBuild(
			commandContext,
			logger,
			targetPatterns,
			graph,
			selection.AllTargets,
			config.Global.StreamLogs,
			config.Global.GetLoadOutputsMode(),
			"verify",
		)
	},
}

// AddVerifyCmd registers the change-aware verification command.
func AddVerifyCmd(rootCmd *cobra.Command) {
	verifyCmd.Flags().StringVar(&verifyOptions.since, "since", "HEAD", "Git ref or Jujutsu revision to compare against")
	rootCmd.AddCommand(verifyCmd)
}
