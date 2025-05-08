package cmds

import (
	"fmt"
	"github.com/spf13/cobra"
	"grog/internal/analysis"
	"grog/internal/loading"
	"grog/internal/model"
)

var graphOptions struct {
	includeConfig bool
}

var GraphCmd = &cobra.Command{
	Use:   "graph",
	Short: "Outputs the target dependency graph",
	Run: func(cmd *cobra.Command, args []string) {
		ctx, logger := setupCommand()

		packages, err := loading.LoadPackages(ctx)
		if err != nil {
			logger.Fatalf(
				"could not load packages: %v",
				err)
		}

		targets, err := model.TargetMapFromPackages(packages)
		if err != nil {
			logger.Fatalf("could not create target map: %v", err)
		}

		graph, err := analysis.BuildGraph(targets)
		if err != nil {
			logger.Fatalf("could not build graph: %v", err)
		}

		jsonData, err := graph.MarshalJSON()
		if err != nil {
			logger.Fatalf("could not marshal graph to json: %v", err)
		}
		fmt.Println(string(jsonData))
	},
}

func init() {
	GraphCmd.Flags().BoolVarP(
		&graphOptions.includeConfig,
		"include-config",
		"c",
		false,
		"Include configuration in the output")
}
