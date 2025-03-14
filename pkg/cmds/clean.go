package cmds

import (
	"fmt"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"os"
)

var CleanCmd = &cobra.Command{
	Use:   "clean",
	Short: "Removes build outputs and clears the cache",
	Long:  `Removes build outputs and clears the cache.`,
	Run: func(cmd *cobra.Command, args []string) {
		cacheDir := viper.GetString("cache_dir")

		fmt.Println("Cleaning cache directory:", cacheDir)

		err := os.RemoveAll(cacheDir)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error cleaning cache: %v\n", err)
			os.Exit(1)
		}

		fmt.Println("Cache cleaned successfully.")
	},
}
