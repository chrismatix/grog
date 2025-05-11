package cmds

import (
	"github.com/spf13/cobra"
	"grog/internal/config"
	"grog/internal/label"
	"grog/internal/loading"
	"grog/internal/selection"
	"os"
	"os/exec"
	"path/filepath"
)

var RunCmd = &cobra.Command{
	Use:   "run",
	Short: "Builds and runs a single target's binary output",
	Args:  cobra.ArbitraryArgs,
	Run: func(cmd *cobra.Command, args []string) {
		ctx, logger := setupCommand()

		if len(args) == 0 {
			logger.Fatalf("`%s` requires a target pattern", cmd.UseLine())
		}

		// Split args at "--" into target and command args
		targetArgs := args[:1]
		userCommandArgs := args[1:]

		if len(targetArgs) == 0 {
			logger.Fatalf("`%s` requires a target pattern", cmd.UseLine())
		}

		currentPackagePath, err := config.Global.GetCurrentPackage()
		if err != nil {
			logger.Fatalf("could not get current package: %v", err)
		}

		targetLabel, err := label.ParseTargetLabel(currentPackagePath, targetArgs[0])
		if err != nil {
			logger.Fatalf("could not parse target label: %v", err)
		}

		graph := loading.MustLoadGraphForBuild(ctx, logger)
		runTarget, hasTarget := graph.GetVertices()[targetLabel]
		if !hasTarget {
			logger.Fatalf("could not find target %s", targetLabel)
		}

		if !runTarget.HasBinOutput() {
			logger.Fatalf("target %s does not have a binary output.", targetLabel)
		}

		// Turn the single target label into a pattern for the build func
		// TODO eventually we might use the worker pool to run multiple t
		targetPattern := label.TargetPatternFromLabel(targetLabel)
		runBuild(ctx, logger, []label.TargetPattern{targetPattern}, graph, selection.NonTestOnly)

		// Run the target output
		binOutputPath := config.GetPathAbsoluteToWorkspaceRoot(
			filepath.Join(runTarget.Label.Package, runTarget.BinOutput.Identifier),
		)
		logger.Infof("Running %s -> %s with args %s", runTarget.Label, runTarget.BinOutput.Identifier, userCommandArgs)

		runCommand := exec.Command(binOutputPath, userCommandArgs...)
		runCommand.Stdout = os.Stdout
		runCommand.Stderr = os.Stderr

		if err := runCommand.Run(); err != nil {
			logger.Fatalf("failed to run binary: %v", err)
		}
	},
}
