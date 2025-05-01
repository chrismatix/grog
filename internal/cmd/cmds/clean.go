package cmds

import (
	"github.com/spf13/cobra"
	"grog/internal/caching/backends"
	"grog/internal/config"
)

var expunge bool

func GetCleanCmd() *cobra.Command {
	var CleanCmd = &cobra.Command{
		Use:   "clean",
		Short: "Removes build outputs and clears the cache",
		Long:  `Removes build outputs and clears the cache.`,
		Run: func(cmd *cobra.Command, args []string) {
			ctx, logger := setupCommand()

			cache, err := backends.GetCacheBackend(ctx, config.Global.Cache)

			if err != nil {
				logger.Fatalf("could not get cache: %v", err)
			}

			err = cache.Clear(ctx, expunge)
			if err != nil {
				logger.Fatalf("could not clear cache: %v", err)
			}

			if expunge {
				logger.Infof("Cache (%s) expunged successfully.", cache.TypeName())
				return
			}
			logger.Info("Workspace cache cleaned successfully.")
		},
	}

	CleanCmd.Flags().BoolVarP(&expunge, "expunge", "e", false, "Expunge all cached artifacts")

	return CleanCmd
}
