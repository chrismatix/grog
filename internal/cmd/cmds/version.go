package cmds

import (
	"fmt"
	"github.com/spf13/cobra"
)

var VersionCmd = &cobra.Command{
	Use:   "version",
	Short: "Print the version info",
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Println(cmd.VersionTemplate())
	},
}
