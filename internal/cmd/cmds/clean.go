package cmds

import (
	"github.com/spf13/cobra"
	"grog/internal/config"
	"os"
)

var expunge bool

var CleanCmd = &cobra.Command{
	Use:   "clean",
	Short: "Removes all cached artifacts.",
	Long:  `Removes cached artifacts from the workspace or the entire grog cache.
By default, only the workspace-specific cache is cleaned. Use the --expunge flag to remove all cached artifacts.`,
	Example: `  grog clean            # Clean the workspace cache
  grog clean --expunge   # Clean the entire grog cache`,
	Args:  cobra.NoArgs,
	Run: func(cmd *cobra.Command, args []string) {
		_, logger := setupCommand()

		var dirToClear string
		if expunge {
			dirToClear = config.Global.Root
		} else {
			dirToClear = config.Global.GetWorkspaceRootDir()
		}

		if err := os.RemoveAll(dirToClear); err != nil {
			logger.Fatalf("Clean failed: %v", err)
		}

		if err := os.MkdirAll(dirToClear, 0755); err != nil {
			logger.Fatalf("Clean succeeded but failed to recreate the directory: %v", err)
		}

		if expunge {
			logger.Info("Cache expunged successfully.")
		} else {
			logger.Info("Workspace cache cleaned successfully.")
		}
	},
}

func AddCleanCmd(rootCmd *cobra.Command) {
	CleanCmd.Flags().BoolVarP(&expunge, "expunge", "e", false, "Expunge all cached artifacts")
	rootCmd.AddCommand(CleanCmd)
}
