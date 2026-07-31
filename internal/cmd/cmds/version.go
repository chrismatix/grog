package cmds

import (
	"grog/internal/console"

	"github.com/spf13/cobra"
)

var VersionCmd = &cobra.Command{
	Use:     "version",
	Short:   "Print the version info.",
	Long:    `Displays the current version of the grog CLI tool.`,
	Example: `  grog version  # Show the version information`,
	Args:    cobra.NoArgs,
	Run: func(cmd *cobra.Command, args []string) {
		if console.JSONEnabled() {
			console.WriteResult(map[string]string{"version": cmd.VersionTemplate()})
			return
		}
		console.WriteText(cmd.VersionTemplate() + "\n")
	},
}
