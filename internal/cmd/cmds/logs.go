package cmds

import (
	"fmt"
	"github.com/spf13/cobra"
	"grog/internal/config"
	"grog/internal/label"
	"grog/internal/loading"
	"grog/internal/logs"
)

var logsOptions struct {
	pathOnly bool
}

var LogsCmd = &cobra.Command{
	Use:   "logs",
	Short: "Print the latest log file for the given target.",
	Long: `Displays the contents of the most recent log file for a specified target.
Use the --path-only flag to only print the path to the log file instead of its contents.`,
	Example: `  grog logs //path/to/package:target       # Show log contents
  grog logs -p //path/to/package:target      # Show only the log file path`,
	Args:              cobra.ExactArgs(1), // Requires exactly one target argument
	ValidArgsFunction: targetLabelCompletion,
	Run: func(cmd *cobra.Command, args []string) {
		ctx, logger := setupCommand()

		currentPackagePath, err := config.Global.GetCurrentPackage()
		if err != nil {
			logger.Fatalf("could not get current package: %v", err)
		}

		targetLabel, err := label.ParseTargetLabel(currentPackagePath, args[0])
		if err != nil {
			logger.Fatalf("could not parse target label: %v", err)
		}

		graph := loading.MustLoadGraphForQuery(ctx, logger)
		logTarget, hasTarget := graph.GetVertices()[targetLabel]
		if !hasTarget {
			logger.Fatalf("could not find target %s", targetLabel)
		}

		targetLogFile := logs.NewTargetLogFile(*logTarget)

		if targetLogFile.Exists() == false {
			logger.Fatalf("no log file found for target %s", targetLabel)
		}

		if logsOptions.pathOnly == true {
			fmt.Println(targetLogFile.Path())
			return
		}

		if err := targetLogFile.Print(); err != nil {
			logger.Fatalf("could not print log file: %v", err)
		}
	},
}

func AddListCmd(cmd *cobra.Command) {
	LogsCmd.Flags().BoolVarP(
		&logsOptions.pathOnly,
		"path-only",
		"p",
		false,
		"Only print out the path of the target logs")

	cmd.AddCommand(LogsCmd)
}
