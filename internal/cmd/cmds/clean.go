package cmds

import (
	"fmt"
	"grog/internal/config"
	"grog/internal/console"
	"grog/internal/locking"
	"os"
	"strings"

	"github.com/spf13/cobra"
)

var (
	expunge    bool
	cleanForce bool
)

var CleanCmd = &cobra.Command{
	Use:   "clean",
	Short: "Removes all cached artifacts.",
	Long: `Removes cached artifacts from the workspace or the entire grog cache.
By default, the target cache and the workspace's logs/lockfile are cleaned. Since the target cache is shared across checkouts of the same repo, running clean affects other workspaces pointing at the same GROG_ROOT. Use the --expunge flag to remove every grog cache directory (CAS included).

Because the cache is shared across checkouts, clean refuses to run while another grog build is in progress in any checkout that uses the same GROG_ROOT — deleting cache entries out from under a running build can make it fail when it reads an output it had already recorded as a cache hit. Pass --force to clean anyway.`,
	Example: `  grog clean            # Clean the target cache and this checkout's logs
  grog clean --expunge   # Clean the entire grog cache`,
	Args: cobra.NoArgs,
	Run: func(cmd *cobra.Command, args []string) {
		_, logger := console.SetupCommand()

		if !cleanForce {
			activeLocks, err := locking.FindActiveLocks(config.Global.Root)
			if err != nil {
				logger.Fatalf("Clean failed: could not check for running builds: %v", err)
			}
			if len(activeLocks) > 0 {
				var message strings.Builder
				fmt.Fprintf(
					&message,
					"Refusing to clean: %d grog build(s) using this cache (%s) are still running:\n",
					len(activeLocks),
					config.Global.Root,
				)
				for _, lock := range activeLocks {
					if lock.Command != "" {
						fmt.Fprintf(&message, "  PID %d: %s\n", lock.ProcessID, lock.Command)
					} else {
						fmt.Fprintf(&message, "  PID %d\n", lock.ProcessID)
					}
				}
				message.WriteString(
					"Cleaning the shared cache now could corrupt those builds. " +
						"Wait for them to finish, or pass --force to clean anyway.",
				)
				logger.Fatal(message.String())
			}
		}

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
	CleanCmd.Flags().BoolVarP(&cleanForce, "force", "f", false, "Clean even if other grog builds are running")
	rootCmd.AddCommand(CleanCmd)
}
