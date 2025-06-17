package cmds

import (
	"fmt"
	"github.com/spf13/cobra"
)

var VersionCmd = &cobra.Command{
	Use:   "version",
	Short: "Print the version info.",
	Long:  `Displays the current version of the grog CLI tool.`,
	Example: `  grog version  # Show the version information`,
	Args:  cobra.NoArgs,
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Println(cmd.VersionTemplate())
	},
}
