package cmds

import (
	"fmt"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"grog/internal/caching/backends"
	"grog/internal/config"
	"os"
	"text/tabwriter"
)

var InfoCmd = &cobra.Command{
	Use:   "info",
	Short: "Prints information about the grog cli and workspace.",
	Long:  `Displays detailed information about the grog CLI configuration, workspace settings, and cache statistics.`,
	Example: `  grog info                   # Show all grog information
  grog info --version          # Show only the version information`,
	Args:  cobra.ArbitraryArgs, // Optional argument for target pattern
	Run: func(cmd *cobra.Command, args []string) {
		ctx, logger := setupCommand()

		version := cmd.VersionTemplate()
		platform := config.Global.GetPlatform()

		writer := tabwriter.NewWriter(os.Stdout, 0, 0, 1, ' ', 0)
		defer writer.Flush()

		fsCache, err := backends.NewFileSystemCache(ctx)
		if err != nil {
			logger.Fatalf("could not instantiate cache: %v", err)
		}

		fmt.Fprintf(writer, "Version:\t%s\n", version)
		fmt.Fprintf(writer, "Platform:\t%s\n", platform)
		fmt.Fprintf(writer, "Workspace:\t%s\n", config.Global.WorkspaceRoot)
		fmt.Fprintf(writer, "Workspace Cache:\t%s\n", config.Global.GetWorkspaceCacheDirectory())
		fmt.Fprintf(writer, "Config:\t%s\n", viper.ConfigFileUsed())
		fmt.Fprintf(writer, "Grog root:\t%s\n", config.Global.Root)

		workspaceCacheSizeBytes, err := fsCache.GetWorkspaceCacheSizeBytes()
		if err != nil {
			logger.Fatalf("could not get workspace cache size: %v", err)
		}
		workspaceCacheSizeMB := float64(workspaceCacheSizeBytes) / (1024 * 1024)
		fmt.Fprintf(writer, "Local workspace cache size:\t%.2f MB\n", workspaceCacheSizeMB)

		totalCacheSizeBytes, err := fsCache.GetCacheSizeBytes()
		if err != nil {
			logger.Fatalf("could not get cache size:\t%v", err)
		}
		totalCacheSizeMB := float64(totalCacheSizeBytes) / (1024 * 1024)
		fmt.Fprintf(writer, "Grog root size:\t%.2f MB\n", totalCacheSizeMB)

	},
}
