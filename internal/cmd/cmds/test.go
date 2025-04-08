package cmds

import (
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"grog/internal/console"
	"grog/internal/label"
)

var TestCmd = &cobra.Command{
	Use:   "test",
	Short: "Loads the user configuration and executes test targets",
	Long:  `Loads the user configuration, checks which targets need to be rebuilt based on file hashes, builds the dependency graph, and executes targets.`,
	Args:  cobra.MaximumNArgs(1), // Optional argument for target pattern
	Run: func(cmd *cobra.Command, args []string) {
		logger := console.InitLogger()

		// Add fail_fast flag
		cmd.PersistentFlags().Bool("fail_fast", false, "Fail fast on first error")
		err := viper.BindPFlag("fail_fast", cmd.PersistentFlags().Lookup("fail_fast"))
		if err != nil {
			logger.Fatal(err)
		}

		if len(args) > 0 {
			targetPattern, err := label.ParseTargetPattern(args[0])
			if err != nil {
				logger.Fatalf("could not parse target pattern: %v", err)
			}
			runBuild(
				targetPattern,
				true,
				true)
		} else {
			// No target pattern: build all targets
			runBuild(
				label.TargetPattern{},
				false,
				true,
			)
		}
	},
}
