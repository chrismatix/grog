package cmds

import (
	"grog/internal/caching"
	"grog/internal/caching/backends"
	"grog/internal/completions"
	"grog/internal/config"
	"grog/internal/console"
	"grog/internal/execution"
	"grog/internal/label"
	"grog/internal/loading"
	"grog/internal/model"
	"grog/internal/output"
	"grog/internal/selection"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/spf13/cobra"
)

var runOptions struct {
	inPackage bool
}

var RunCmd = &cobra.Command{
	Use:   "run",
	Short: "Builds and runs a single target's binary output.",
	Long: `Builds a single target that produces a binary output and then executes it with the provided arguments.
Any arguments after the target are passed directly to the binary being executed.`,
	Example: `  grog run //path/to/package:target           # Run the target
  grog run //path/to/package:target arg1 arg2   # Run with arguments
  grog run -i //path/to/package:target          # Run in the package directory`,
	Args:              cobra.ArbitraryArgs,
	ValidArgsFunction: completions.BinaryTargetPatternCompletion,
	Run: func(cmd *cobra.Command, args []string) {
		ctx, logger := console.SetupCommand()

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
		node, hasNode := graph.GetNodes()[targetLabel]
		if !hasNode {
			logger.Fatalf("could not find target %s", targetLabel)
		}

		var runTarget *model.Target
		castNode, isTarget := node.(*model.Target)
		if !isTarget {
			// For an alias use the actual target
			castAlias, isAlias := node.(*model.Alias)
			if !isAlias {
				logger.Fatalf("%s is not a target", targetLabel)
			}

			resolvedNode := graph.GetNodes()[castAlias.Actual]
			resolvedTarget, isTarget := resolvedNode.(*model.Target)
			if !isTarget {
				logger.Fatalf("%s resolved from %s is not a target", targetLabel, castAlias.Actual)
			} else {
				runTarget = resolvedTarget
			}
		} else {
			runTarget = castNode
		}

		if !runTarget.HasBinOutput() {
			logger.Fatalf("target %s does not have a binary output.", targetLabel)
		}

		// Turn the single target label into a pattern for the build func
		// TODO eventually we might use the worker pool to run multiple build outputs
		targetPattern := label.TargetPatternFromLabel(targetLabel)
		runBuild(
			ctx,
			logger,
			[]label.TargetPattern{targetPattern},
			graph,
			selection.NonTestOnly,
			config.Global.StreamLogs,
			config.Global.GetLoadOutputsMode(),
		)

		// If we ran in load_outputs=mininal mode we might still need to load more outputs
		if config.Global.GetLoadOutputsMode() == config.LoadOutputsMinimal {

			cache, err := backends.GetCacheBackend(ctx, config.Global.Cache)
			if err != nil {
				logger.Fatalf("could not instantiate cache: %v", err)
			}
			targetCache := caching.NewTargetCache(cache)
			registry := output.NewRegistry(targetCache, config.Global.EnableCache)

			executor := execution.NewExecutor(targetCache, registry, graph, config.Global.FailFast, config.Global.StreamLogs, config.Global.GetLoadOutputsMode())
			logger.Infof("Loading outputs of direct dependencies due to load_outputs=minimal")
			err = executor.LoadDependencyOutputs(ctx, runTarget, func(_ string) {})
			if err != nil {
				logger.Fatalf("could not load dependencies: %v", err)
			}
		}
		// Run the target output
		binOutputPath := config.GetPathAbsoluteToWorkspaceRoot(
			filepath.Join(runTarget.Label.Package, runTarget.BinOutput.Identifier),
		)

		runCommand := exec.Command(binOutputPath, userCommandArgs...)
		runCommand.Env = execution.GetExtendedTargetEnv(ctx, runTarget)
		runCommand.Stdout = os.Stdout
		runCommand.Stderr = os.Stderr
		runCommand.Stdin = os.Stdin

		if runOptions.inPackage {
			packagePath := config.GetPathAbsoluteToWorkspaceRoot(runTarget.Label.Package)
			runCommand.Dir = packagePath
			logger.Infof("Running %s -> %s with args %s in package directory", runTarget.Label, runTarget.BinOutput.Identifier, userCommandArgs)
		} else {
			logger.Infof("Running %s -> %s with args %s", runTarget.Label, runTarget.BinOutput.Identifier, userCommandArgs)
		}

		if err := runCommand.Run(); err != nil {
			logger.Fatalf("failed to run binary: %v", err)
		}
	},
}

func AddRunCmd(cmd *cobra.Command) {
	RunCmd.Flags().BoolVarP(&runOptions.inPackage, "in-package", "i", false, "Run the target in the package directory where it is defined.")
	cmd.AddCommand(RunCmd)
}
