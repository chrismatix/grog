package cmds

import (
	"fmt"
	"github.com/spf13/cobra"
	"grog/pkg/config"
	"grog/pkg/loading"
)

var BuildCmd = &cobra.Command{
	Use:   "build",
	Short: "Loads the build configuration and executes targets",
	Long:  `Loads the build configuration, checks which targets need to be rebuilt based on file hashes, builds the dependency graph, and executes targets.`,
	Run: func(cmd *cobra.Command, args []string) {
		logger := config.GetLogger()

		packages, err := loading.LoadPackages()
		if err != nil {
			logger.Fatalf(
				"could not load packages: %v",
				err)
		}
		fmt.Println(packages)
	},
}
