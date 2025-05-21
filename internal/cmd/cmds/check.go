package cmds

import (
	"github.com/spf13/cobra"
	"grog/internal/analysis"
	"grog/internal/loading"
	"os"
)

var CheckCmd = &cobra.Command{
	Use:   "check",
	Short: "Loads the build graph and runs basic consistency checks.",
	Long:  `Loads the the build graph and performs the same consistency checks as 'grog build' without actually building anything.`,
	Args:  cobra.NoArgs,
	Run: func(cmd *cobra.Command, args []string) {
		ctx, logger := setupCommand()

		graph := loading.MustLoadGraphForBuild(ctx, logger)

		errs := analysis.CheckTargetConstraints(logger, graph.GetVertices())
		if len(errs) > 0 {
			for _, err := range errs {
				logger.Errorf(err.Error())
			}
			os.Exit(1)
		}

		logger.Infof("Build graph is valid.")
	},
}
