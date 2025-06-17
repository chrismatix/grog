package cmds

import (
	"github.com/spf13/cobra"
	"grog/internal/completions"
	"grog/internal/config"
	"grog/internal/label"
	"grog/internal/loading"
	"grog/internal/model"
	"grog/internal/selection"
)

var rDepsOptions struct {
	transitive bool
	targetType string
}

var RDepsCmd = &cobra.Command{
	Use:   "rdeps",
	Short: "Lists (transitive) dependants (reverse dependencies) of a target.",
	Long: `Lists the direct or transitive dependants (reverse dependencies) of a specified target.
By default, only direct dependants are shown. Use the --transitive flag to show all transitive dependants.
Dependants can be filtered by target type using the --target-type flag.`,
	Example: `  grog rdeps //path/to/package:target           # Show direct dependants
  grog rdeps -t //path/to/package:target          # Show transitive dependants
  grog rdeps --target-type=test //path/to/package:target  # Show only test dependants`,
	Args:              cobra.MaximumNArgs(1),
	ValidArgsFunction: completions.TargetLabelCompletion,
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

		var rDeps []*model.Target
		if rDepsOptions.transitive {
			rDeps = graph.GetDescendants(target)
		} else {
			rDeps = graph.GetDependants(target)
		}

		targetTypeFilter, err := selection.StringToTargetTypeSelection(rDepsOptions.targetType)
		if err != nil {
			logger.Fatalf(err.Error())
		}
		selector := selection.New(nil, config.Global.Tags, config.Global.ExcludeTags, targetTypeFilter)
		filteredRDeps := selector.FilterTargets(rDeps)

		model.PrintSortedLabels(filteredRDeps)
	},
}

func AddRDepsCmd(rootCmd *cobra.Command) {
	RDepsCmd.Flags().BoolVarP(
		&rDepsOptions.transitive,
		"transitive",
		"t",
		false,
		"Include all transitive dependants of the target")

	RDepsCmd.Flags().StringVar(
		&rDepsOptions.targetType,
		"target-type",
		"all",
		"Filter targets by type (all, test, no_test, bin_output)")

	rootCmd.AddCommand(RDepsCmd)
}
