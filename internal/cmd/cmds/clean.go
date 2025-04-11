package cmds

import (
	"github.com/spf13/cobra"
	"grog/internal/caching"
	"grog/internal/console"
)

var expunge bool

func GetCleanCmd() *cobra.Command {
	var CleanCmd = &cobra.Command{
		Use:   "clean",
		Short: "Removes build outputs and clears the cache",
		Long:  `Removes build outputs and clears the cache.`,
		Run: func(cmd *cobra.Command, args []string) {
			logger := console.InitLogger()
			cache, err := caching.GetCache(logger)
			if err != nil {
				logger.Fatalf("could not get cache: %v", err)
			}

			err = cache.Clear(expunge)
			if err != nil {
				logger.Fatalf("could not clear cache: %v", err)
			}

			if expunge {
				logger.Info("Cache expunged successfully.")
				return
			}
			logger.Info("Workspace cache cleaned successfully.")
		},
	}

	CleanCmd.Flags().BoolVarP(&expunge, "expunge", "e", false, "Expunge all cached artifacts")

	return CleanCmd
}
