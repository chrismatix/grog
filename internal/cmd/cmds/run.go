package cmds

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sync"

	"github.com/spf13/cobra"

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
	"grog/internal/worker"
)

var runOptions struct {
	inPackage bool
}

var RunCmd = &cobra.Command{
	Use:   "run",
	Short: "Builds and runs one or more targets' binary outputs.",
	Long: `Builds targets that produce binary outputs and then executes them with the provided arguments.
Use "--" to separate the list of targets from the arguments passed to the binaries.`,
	Example: `  grog run //path/to/package:target -- arg1 arg2   # Run with arguments
  grog run //path/to/package:target //path:other --      # Run multiple targets
  grog run -i //path/to/package:target -- arg1 arg2      # Run in the package directory`,
	Args:              cobra.ArbitraryArgs,
	ValidArgsFunction: completions.BinaryTargetPatternCompletion,
	Run: func(cmd *cobra.Command, args []string) {
		ctx, logger := console.SetupCommand()

		if len(args) == 0 {
			logger.Fatalf("`%s` requires a target pattern", cmd.UseLine())
		}

		targetArgs, userCommandArgs, err := splitRunArgs(args, cmd.ArgsLenAtDash())
		if err != nil {
			logger.Fatalf("%v", err)
		}
		if len(targetArgs) == 0 {
			logger.Fatalf("`%s` requires a target pattern", cmd.UseLine())
		}

		currentPackagePath, err := config.Global.GetCurrentPackage()
		if err != nil {
			logger.Fatalf("could not get current package: %v", err)
		}

		if len(targetArgs) > 1 {
			targetLabels := parseMultipleTargetLabels(logger, currentPackagePath, targetArgs)
			runTargetsByLabels(ctx, logger, targetLabels, userCommandArgs)
			return
		}

		targetArg := targetArgs[0]
		if targetLabel, parseErr := label.ParseTargetLabel(currentPackagePath, targetArg); parseErr == nil {
			runTargetsByLabels(ctx, logger, []label.TargetLabel{targetLabel}, userCommandArgs)
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

func runScriptFile(ctx context.Context, logger *console.Logger, scriptArg string, userCommandArgs []string) error {
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

	buildAndRunTargets(ctx, logger, graph, []*model.Target{target}, userCommandArgs)
	return nil
}

func buildAndRunTargets(ctx context.Context, logger *console.Logger, graph *dag.DirectedTargetGraph, runTargets []*model.Target, userCommandArgs []string) {
	if len(runTargets) == 0 {
		return
	}
	var targetPatterns []label.TargetPattern
	patternSet := make(map[label.TargetPattern]struct{})
	for _, runTarget := range runTargets {
		pattern := label.TargetPatternFromLabel(runTarget.Label)
		if _, exists := patternSet[pattern]; exists {
			continue
		}
		patternSet[pattern] = struct{}{}
		targetPatterns = append(targetPatterns, pattern)
	}

	RunBuild(
		ctx,
		logger,
		targetPatterns,
		graph,
		selection.NonTestOnly,
		config.Global.StreamLogs,
		config.Global.GetLoadOutputsMode(),
	)

	for _, runTarget := range runTargets {
		loadDependencyOutputsIfNeeded(ctx, logger, graph, runTarget)
	}

	if err := runTargetBinaries(ctx, logger, runTargets, userCommandArgs); err != nil {
		logger.Fatalf("%v", err)
	}
}

func loadDependencyOutputsIfNeeded(ctx context.Context, logger *console.Logger, graph *dag.DirectedTargetGraph, runTarget *model.Target) {
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
	registry := output.NewRegistry(ctx, cas)

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
	if err := executor.LoadDependencyOutputs(ctx, runTarget, func(_ worker.StatusUpdate) {}); err != nil {
		logger.Fatalf("could not load dependencies: %v", err)
	}
}

func splitRunArgs(args []string, argsLenAtDash int) ([]string, []string, error) {
	if argsLenAtDash == 0 {
		return nil, nil, fmt.Errorf("expected a target pattern before '--'")
	}
	if argsLenAtDash > 0 {
		return args[:argsLenAtDash], args[argsLenAtDash:], nil
	}
	// (== -1) This means that no '--' was found and we interpret all remaining args as targets
	return args, nil, nil
}

func parseMultipleTargetLabels(logger *console.Logger, currentPackagePath string, targetArgs []string) []label.TargetLabel {
	labels := make([]label.TargetLabel, 0, len(targetArgs))
	seen := make(map[label.TargetLabel]struct{})
	for _, targetArg := range targetArgs {
		targetLabel, err := label.ParseTargetLabel(currentPackagePath, targetArg)
		if err != nil {
			logger.Fatalf("could not parse target label %s: %v. Use '--' to separate target labels from binary arguments.", targetArg, err)
		}
		if _, exists := seen[targetLabel]; exists {
			continue
		}
		seen[targetLabel] = struct{}{}
		labels = append(labels, targetLabel)
	}
	return labels
}

func runTargetsByLabels(ctx context.Context, logger *console.Logger, targetLabels []label.TargetLabel, userCommandArgs []string) {
	graph := loading.MustLoadGraphForBuild(ctx, logger)
	runTargets := make([]*model.Target, 0, len(targetLabels))
	seen := make(map[label.TargetLabel]struct{})
	for _, targetLabel := range targetLabels {
		runTarget := resolveRunTarget(logger, graph, targetLabel)
		if _, exists := seen[runTarget.Label]; exists {
			continue
		}
		seen[runTarget.Label] = struct{}{}
		runTargets = append(runTargets, runTarget)
	}

	buildAndRunTargets(ctx, logger, graph, runTargets, userCommandArgs)
}

func resolveRunTarget(logger *console.Logger, graph *dag.DirectedTargetGraph, targetLabel label.TargetLabel) *model.Target {
	node, hasNode := graph.GetNodes()[targetLabel]
	if !hasNode {
		logger.Fatalf("could not find target %s", targetLabel)
	}

	switch typed := node.(type) {
	case *model.Target:
		if !typed.HasBinOutput() {
			logger.Fatalf("target %s does not have a binary output.", targetLabel)
		}
		return typed
	case *model.Alias:
		resolvedNode := graph.GetNodes()[typed.Actual]
		resolvedTarget, ok := resolvedNode.(*model.Target)
		if !ok {
			logger.Fatalf("%s resolved from %s is not a target", targetLabel, typed.Actual)
		}
		if !resolvedTarget.HasBinOutput() {
			logger.Fatalf("target %s does not have a binary output.", typed.Actual)
		}
		return resolvedTarget
	default:
		logger.Fatalf("%s is not a target", targetLabel)
	}
	return nil
}

func runTargetBinaries(ctx context.Context, logger *console.Logger, runTargets []*model.Target, userCommandArgs []string) error {
	if len(runTargets) == 0 {
		return nil
	}

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	type binaryRun struct {
		target *model.Target
		cmd    *exec.Cmd
	}
	// For each target, create a new Cmd and run it in a goroutine.
	runs := make([]binaryRun, 0, len(runTargets))
	for _, runTarget := range runTargets {
		cmd := newBinaryRunCommand(ctx, runTarget, userCommandArgs)
		if runOptions.inPackage {
			logger.Infof("Running %s -> %s with args %s in package directory", runTarget.Label, runTarget.BinOutput.Identifier, userCommandArgs)
		} else {
			logger.Infof("Running %s -> %s with args %s", runTarget.Label, runTarget.BinOutput.Identifier, userCommandArgs)
		}
		runs = append(runs, binaryRun{target: runTarget, cmd: cmd})
	}

	errCh := make(chan error, len(runs))
	var wg sync.WaitGroup
	for _, run := range runs {
		wg.Add(1)
		go func(run binaryRun) {
			defer wg.Done()
			if err := run.cmd.Run(); err != nil {
				errCh <- fmt.Errorf("failed to run %s: %w", run.target.Label, err)
				cancel()
			}
		}(run)
	}

	wg.Wait()
	close(errCh)
	var errs []error
	for err := range errCh {
		errs = append(errs, err)
	}
	if len(errs) == 0 {
		return nil
	}
	if len(runTargets) == 1 {
		return errs[0]
	}
	return fmt.Errorf("multiple binaries failed: %w", errors.Join(errs...))
}

func newBinaryRunCommand(ctx context.Context, runTarget *model.Target, userCommandArgs []string) *exec.Cmd {
	binOutputPath := config.GetPathAbsoluteToWorkspaceRoot(
		filepath.Join(runTarget.Label.Package, runTarget.BinOutput.Identifier),
	)
	runCommand := exec.CommandContext(ctx, binOutputPath, userCommandArgs...)
	runCommand.Env = execution.GetExtendedTargetEnv(ctx, runTarget)
	runCommand.Stdout = os.Stdout
	runCommand.Stderr = os.Stderr
	runCommand.Stdin = os.Stdin
	if runOptions.inPackage {
		packagePath := config.GetPathAbsoluteToWorkspaceRoot(runTarget.Label.Package)
		runCommand.Dir = packagePath
	}
	return runCommand
}
