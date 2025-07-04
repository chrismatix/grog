package cmds

import (
	"github.com/spf13/cobra"
	"grog/internal/analysis"
	"grog/internal/console"
	"grog/internal/loading"
	"os"
)

var CheckCmd = &cobra.Command{
	Use:     "check",
	Short:   "Loads the build graph and runs basic consistency checks.",
	Long:    `Loads the build graph and performs the same consistency checks as 'grog build' without actually building anything.`,
	Example: `  grog check  # Validate the build graph for consistency issues`,
	Args:    cobra.NoArgs,
	Run: func(cmd *cobra.Command, args []string) {
		ctx, logger := console.SetupCommand()

		graph := loading.MustLoadGraphForBuild(ctx, logger)

		errs := analysis.CheckTargetConstraints(logger, graph.GetNodes())
		if len(errs) > 0 {
			for _, err := range errs {
				logger.Errorf(err.Error())
			}
			os.Exit(1)
		}

		logger.Infof("Build graph is valid.")
	},
}
