package cmds

import (
	"github.com/spf13/cobra"
	"grog/internal/config"
	"grog/internal/console"
	"grog/internal/label"
	"grog/internal/loading"
	"grog/internal/model"
	"path/filepath"
)

var OwnersCmd = &cobra.Command{
	Use:   "owners",
	Short: "Lists targets that own the specified files as inputs.",
	Long: `Identifies and lists all targets that include the specified files as inputs.
This is useful for finding which targets will be affected by changes to specific files.`,
	Example: `  grog owners path/to/file.txt                # Find targets that use a specific file
  grog owners path/to/file1.txt path/to/file2.txt  # Find targets that use any of the specified files`,
	Args: cobra.MinimumNArgs(1), // Require at least one file argument
	Run: func(cmd *cobra.Command, args []string) {
		ctx, logger := console.SetupCommand()

		graph := loading.MustLoadGraphForQuery(ctx, logger)

		targets := graph.GetVertices()

		// Resolve the input files relative to current directory
		for i, arg := range args {
			absPath, err := filepath.Abs(arg)
			if err != nil {
				logger.Fatalf("Failed to get absolute path for %s: %v", arg, err)
			}
			args[i] = absPath
		}

		inputFileMatches := func(absInputPath string) bool {
			for _, arg := range args {
				if absInputPath == arg {
					return true
				}
			}
			return false
		}

		// Find targets that have any of the specified files in their inputs
		var matchingTargets []*model.Target
		for _, target := range targets {
			for _, inputFile := range target.Inputs {
				// Get the absolute path of the input file
				absInputPath := config.GetPathAbsoluteToWorkspaceRoot(filepath.Join(
					target.Label.Package,
					inputFile,
				))

				if inputFileMatches(absInputPath) {
					matchingTargets = append(matchingTargets, target)
					break // Found a match, no need to check other args
				}
			}
		}

		var matchingLabels []label.TargetLabel
		for _, target := range matchingTargets {
			matchingLabels = append(matchingLabels, target.Label)
		}

		label.PrintSorted(matchingLabels)
	},
}

func AddOwnersCmd(rootCmd *cobra.Command) {
	rootCmd.AddCommand(OwnersCmd)
}
