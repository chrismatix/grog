package cmds

import (
	"github.com/spf13/cobra"
	"grog/internal/config"
	"grog/internal/label"
	"grog/internal/loading"
)

var rDepsOptions struct {
	transitive bool
}

var RDepsCmd = &cobra.Command{
	Use:   "rdeps",
	Short: "Lists (transitive) dependants (reverse dependencies) of a target.",
	Args:  cobra.MaximumNArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		ctx, logger := setupCommand()

		if len(args) == 0 {
			logger.Fatalf("`%s` requires a target label", cmd.UseLine())
		}

		currentPackagePath, err := config.Global.GetCurrentPackage()
		if err != nil {
			logger.Fatalf("could not get current package: %v", err)
		}

		targetLabel, err := label.ParseTargetLabel(currentPackagePath, args[0])
		if err != nil {
			logger.Fatalf("could not parse target label: %v", err)
		}
		graph := loading.MustLoadGraphForQuery(ctx, logger)

		target, hasTarget := graph.GetVertices()[targetLabel]
		if !hasTarget {
			logger.Fatalf("could not find target %s", targetLabel)
		}

		var rDeps []label.TargetLabel
		if rDepsOptions.transitive {
			for _, descendant := range graph.GetDescendants(target) {
				rDeps = append(rDeps, descendant.Label)
			}
		} else {
			dependants, err := graph.GetDependants(target)
			if err != nil {
				logger.Fatalf("could not get in edges: %v", err)
			}

			for _, dependant := range dependants {
				rDeps = append(rDeps, dependant.Label)
			}
		}

		label.PrintSorted(rDeps)
	},
}

func AddRDepsCmd(rootCmd *cobra.Command) {
	RDepsCmd.Flags().BoolVarP(
		&rDepsOptions.transitive,
		"transitive",
		"t",
		false,
		"Include all transitive dependants of the target")

	rootCmd.AddCommand(RDepsCmd)
}
