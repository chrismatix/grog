package cmds

import (
	"github.com/spf13/cobra"
	"grog/internal/completions"
	"grog/internal/config"
	"grog/internal/console"
	"grog/internal/label"
	"grog/internal/loading"
	"grog/internal/model"
	"grog/internal/selection"
)

var depsOptions struct {
	transitive bool
	targetType string
}

var DepsCmd = &cobra.Command{
	Use:   "deps",
	Short: "Lists (transitive) dependencies of a target.",
	Long: `Lists the direct or transitive dependencies of a specified target.
By default, only direct dependencies are shown. Use the --transitive flag to show all transitive dependencies.
Dependencies can be filtered by target type using the --target-type flag.`,
	Example: `  grog deps //path/to/package:target           # Show direct dependencies
  grog deps -t //path/to/package:target          # Show transitive dependencies
  grog deps --target-type=test //path/to/package:target  # Show only test dependencies`,
	Args:              cobra.MaximumNArgs(1),
	ValidArgsFunction: completions.AllTargetPatternCompletion,
	Run: func(cmd *cobra.Command, args []string) {
		ctx, logger := console.SetupCommand()

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

		var dependencies []*model.Target
		if depsOptions.transitive {
			dependencies = graph.GetAncestors(target)
		} else {
			dependencies = graph.GetDependencies(target)
		}

		// Filter by target type
		targetTypeFilter, err := selection.StringToTargetTypeSelection(depsOptions.targetType)
		if err != nil {
			logger.Fatalf(err.Error())
		}
		selector := selection.New(nil, config.Global.Tags, config.Global.ExcludeTags, targetTypeFilter)
		filteredDeps := selector.FilterTargets(dependencies)

		model.PrintSortedLabels(filteredDeps)
	},
}

func AddDepsCmd(rootCmd *cobra.Command) {
	DepsCmd.Flags().BoolVarP(
		&depsOptions.transitive,
		"transitive",
		"t",
		false,
		"Include all transitive dependencies of the target")

	DepsCmd.Flags().StringVar(
		&depsOptions.targetType,
		"target-type",
		"all",
		"Filter targets by type (all, test, no_test, bin_output)")

	rootCmd.AddCommand(DepsCmd)
}
