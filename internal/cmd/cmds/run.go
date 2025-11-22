package cmds

import (
	"context"
	"fmt"
	"grog/internal/caching"
	"grog/internal/caching/backends"
	"grog/internal/completions"
	"grog/internal/config"
	"grog/internal/console"
	"grog/internal/dag"
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
	"go.uber.org/zap"
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

		targetArg := args[0]
		userCommandArgs := args[1:]

		currentPackagePath, err := config.Global.GetCurrentPackage()
		if err != nil {
			logger.Fatalf("could not get current package: %v", err)
		}

		if targetLabel, parseErr := label.ParseTargetLabel(currentPackagePath, targetArg); parseErr == nil {
			runTargetByLabel(ctx, logger, targetLabel, userCommandArgs)
			return
		}

		if err := runScriptFile(ctx, logger, targetArg, userCommandArgs); err != nil {
			logger.Fatalf("%v", err)
		}
	},
}

func AddRunCmd(cmd *cobra.Command) {
	RunCmd.Flags().BoolVarP(&runOptions.inPackage, "in-package", "i", false, "Run the target in the package directory where it is defined.")
	cmd.AddCommand(RunCmd)
}

func runTargetByLabel(ctx context.Context, logger *zap.SugaredLogger, targetLabel label.TargetLabel, userCommandArgs []string) {
	graph := loading.MustLoadGraphForBuild(ctx, logger)

	node, hasNode := graph.GetNodes()[targetLabel]
	if !hasNode {
		logger.Fatalf("could not find target %s", targetLabel)
	}

	var runTarget *model.Target
	switch typed := node.(type) {
	case *model.Target:
		runTarget = typed
	case *model.Alias:
		resolvedNode := graph.GetNodes()[typed.Actual]
		resolvedTarget, ok := resolvedNode.(*model.Target)
		if !ok {
			logger.Fatalf("%s resolved from %s is not a target", targetLabel, typed.Actual)
		}
		runTarget = resolvedTarget
	default:
		logger.Fatalf("%s is not a target", targetLabel)
	}

	if !runTarget.HasBinOutput() {
		logger.Fatalf("target %s does not have a binary output.", targetLabel)
	}

	buildAndRunTarget(ctx, logger, graph, runTarget, userCommandArgs)
}

func runScriptFile(ctx context.Context, logger *zap.SugaredLogger, scriptArg string, userCommandArgs []string) error {
	scriptPath, err := filepath.Abs(scriptArg)
	if err != nil {
		return fmt.Errorf("could not resolve script path %s: %w", scriptArg, err)
	}

	if _, err := os.Stat(scriptPath); err != nil {
		return fmt.Errorf("script %s: %w", scriptArg, err)
	}

	if _, err := config.GetPathRelativeToWorkspaceRoot(scriptPath); err != nil {
		return fmt.Errorf("script %s is outside the workspace: %w", scriptArg, err)
	}

	target, err := loading.LoadScriptTarget(ctx, logger, scriptPath)
	if err != nil {
		return err
	}

	graph := loading.MustLoadGraphForBuild(ctx, logger)
	if existing := graph.GetNodes()[target.Label]; existing != nil {
		return fmt.Errorf("target %s is already defined in the build graph; refer to it by label instead", target.Label)
	}

	graph.AddNode(target)
	for _, dep := range target.Dependencies {
		dependencyNode := graph.GetNodes()[dep]
		if dependencyNode == nil {
			return fmt.Errorf("dependency %s referenced by %s was not found", dep, target.Label)
		}

		if err := graph.AddEdge(dependencyNode, target); err != nil {
			return err
		}
	}

	buildAndRunTarget(ctx, logger, graph, target, userCommandArgs)
	return nil
}

func buildAndRunTarget(ctx context.Context, logger *zap.SugaredLogger, graph *dag.DirectedTargetGraph, runTarget *model.Target, userCommandArgs []string) {
	targetPattern := label.TargetPatternFromLabel(runTarget.Label)
	runBuild(
		ctx,
		logger,
		[]label.TargetPattern{targetPattern},
		graph,
		selection.NonTestOnly,
		config.Global.StreamLogs,
		config.Global.GetLoadOutputsMode(),
	)

	loadDependencyOutputsIfNeeded(ctx, logger, graph, runTarget)
	runTargetBinary(ctx, logger, runTarget, userCommandArgs)
}

func loadDependencyOutputsIfNeeded(ctx context.Context, logger *zap.SugaredLogger, graph *dag.DirectedTargetGraph, runTarget *model.Target) {
	if config.Global.GetLoadOutputsMode() != config.LoadOutputsMinimal {
		return
	}

	cache, err := backends.GetCacheBackend(ctx, config.Global.Cache)
	if err != nil {
		logger.Fatalf("could not instantiate cache: %v", err)
	}
	targetCache := caching.NewTargetResultCache(cache)
	cas := caching.NewCas(cache)
	taintCache := caching.NewTaintCache(cache)
	registry := output.NewRegistry(cas)

	executor := execution.NewExecutor(
		targetCache,
		taintCache,
		registry,
		graph,
		config.Global.FailFast,
		config.Global.StreamLogs,
		config.Global.EnableCache,
		config.Global.GetLoadOutputsMode(),
	)
	logger.Infof("Loading outputs of direct dependencies due to load_outputs=minimal")
	if err := executor.LoadDependencyOutputs(ctx, runTarget, func(_ string) {}); err != nil {
		logger.Fatalf("could not load dependencies: %v", err)
	}
}

func runTargetBinary(ctx context.Context, logger *zap.SugaredLogger, runTarget *model.Target, userCommandArgs []string) {
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
}
