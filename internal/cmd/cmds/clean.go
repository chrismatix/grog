package cmds

import (
	"github.com/spf13/cobra"
	"grog/internal/config"
	"grog/internal/console"
	"os"
)

var expunge bool

var CleanCmd = &cobra.Command{
	Use:   "clean",
	Short: "Removes all cached artifacts.",
	Long: `Removes cached artifacts from the workspace or the entire grog cache.
By default, the target cache and the workspace's logs/lockfile are cleaned. Since the target cache is shared across checkouts of the same repo, running clean affects other workspaces pointing at the same GROG_ROOT. Use the --expunge flag to remove every grog cache directory (CAS included).`,
	Example: `  grog clean            # Clean the target cache and this checkout's logs
  grog clean --expunge   # Clean the entire grog cache`,
	Args: cobra.NoArgs,
	Run: func(cmd *cobra.Command, args []string) {
		_, logger := console.SetupCommand()

		var dirsToClear []string
		if expunge {
			dirsToClear = []string{config.Global.Root}
		} else {
			dirsToClear = []string{
				config.Global.GetWorkspaceRootDir(),
				config.Global.GetWorkspaceCacheDirectory(),
			}
		}

		for _, dir := range dirsToClear {
			if err := os.RemoveAll(dir); err != nil {
				logger.Fatalf("Clean failed: %v", err)
			}
			if err := os.MkdirAll(dir, 0755); err != nil {
				logger.Fatalf("Clean succeeded but failed to recreate the directory: %v", err)
			}
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
