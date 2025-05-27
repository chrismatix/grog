package cmds

import (
	"context"
	"errors"
	"fmt"
	"github.com/fatih/color"
	"github.com/spf13/cobra"
	"go.uber.org/zap"
	"grog/internal/analysis"
	"grog/internal/caching"
	"grog/internal/caching/backends"
	"grog/internal/config"
	"grog/internal/console"
	"grog/internal/dag"
	"grog/internal/execution"
	"grog/internal/label"
	"grog/internal/loading"
	"grog/internal/output"
	"grog/internal/selection"
	"os"
	"strings"
	"time"
)

var buildOptions struct {
	streamLogs bool
}

var BuildCmd = &cobra.Command{
	Use:   "build",
	Short: "Loads the user configuration and executes build targets",
	Long:  `Loads the user configuration, checks which targets need to be rebuilt based on file hashes, builds the dependency graph, and executes targets.`,
	Args:  cobra.ArbitraryArgs, // Optional argument for target pattern
	Run: func(cmd *cobra.Command, args []string) {
		ctx, logger := setupCommand()

		currentPackagePath, err := config.Global.GetCurrentPackage()
		if err != nil {
			logger.Fatalf("could not get current package: %v", err)
		}

		targetPatterns, err := label.ParsePatternsOrMatchAll(currentPackagePath, args)
		if err != nil {
			logger.Fatalf("could not parse target pattern: %v", err)
		}

		graph := loading.MustLoadGraphForBuild(ctx, logger)

		runBuild(ctx, logger, targetPatterns, graph, selection.NonTestOnly, buildOptions.streamLogs)
	},
}

func AddBuildCmd(rootCmd *cobra.Command) {
	BuildCmd.Flags().BoolVarP(
		&buildOptions.streamLogs,
		"stream-logs",
		"t",
		false,
		"Forward target build logs to stdout/-err")

	rootCmd.AddCommand(BuildCmd)
}

// runBuild runs the build/test command for the given target pattern
func runBuild(
	ctx context.Context,
	logger *zap.SugaredLogger,
	targetPatterns []label.TargetPattern,
	graph *dag.DirectedTargetGraph,
	testFilter selection.TargetTypeSelection,
	streamLogs bool,
) {
	startTime := time.Now()
	errs := analysis.CheckTargetConstraints(logger, graph.GetVertices())
	if len(errs) > 0 {
		for _, err := range errs {
			logger.Errorf(err.Error())
		}
		os.Exit(1)
	}

	selector := selection.New(targetPatterns, config.Global.Tags, config.Global.ExcludeTags, testFilter)
	// Select targets based on the target pattern.
	selectedCount, skippedCount, err := selector.SelectTargetsForBuild(graph)
	if err != nil {
		logger.Fatalf("target selection failed: %v", err)
	}

	if selectedCount == 0 {
		// Fail if no targets were selected
		errString := fmt.Sprintf("could not find any targets matching %s", label.PatternSetToString(targetPatterns))
		if skippedCount > 0 {
			errString += fmt.Sprintf(" (%s not matching %s host)",
				console.FCountTargets(skippedCount), config.Global.GetPlatform())
		}
		logger.Fatalf(errString)
	}

	infoStr := fmt.Sprintf("Selected %s.",
		console.FCountTargets(selectedCount))
	if skippedCount > 0 {
		infoStr = fmt.Sprintf("Selected %s (%s not matching %s host).",
			console.FCountTargets(selectedCount),
			console.FCountTargets(skippedCount),
			config.Global.GetPlatform())
	}

	logger.Infof(infoStr)

	failFast := config.Global.FailFast

	cache, err := backends.GetCacheBackend(ctx, config.Global.Cache)
	if err != nil {
		logger.Fatalf("could not instantiate cache: %v", err)
	}
	targetCache := caching.NewTargetCache(cache)
	registry := output.NewRegistry(targetCache)

	completionMap, err := execution.Execute(ctx, registry, graph, failFast, streamLogs)

	elapsedTime := time.Since(startTime).Seconds()
	// Mostly used to keep our test fixtures deterministic
	if !config.Global.DisableNonDeterministicLogging {
		logger.Infof("Elapsed time: %.3fs", elapsedTime)
	}

	if err != nil {
		logger.Errorf("execution failed: %v", err)
		os.Exit(1)
	}

	// small helper for logging
	goal := "Build"
	if testFilter == selection.TestOnly {
		goal = "Test"
	}

	buildErrors := completionMap.GetErrors()
	successCount, cacheHits := completionMap.SuccessCount()

	if len(buildErrors) > 0 {
		logger.Errorf("%s failed. %s completed (%d cache hits), %d failed:",
			goal,
			console.FCountTargets(successCount),
			cacheHits,
			len(buildErrors))

		for target, completion := range completionMap {
			if completion.IsSuccess {
				continue
			}

			var executionError *execution.CommandError
			color.Red("---------------------------------")
			if completion.Err == nil {
				logger.Errorf("Target %s failed with no error", target.Label)
			} else if errors.As(completion.Err, &executionError) {
				logger.Errorf("Target %s failed with exit code %d:\ncommand: \"%s\"\n%s",
					target.Label,
					executionError.ExitCode,
					target.Command,
					strings.TrimSpace(executionError.Output))
			} else {
				logger.Errorf("Target %s failed: %v", target.Label, completion.Err)
			}
		}
		os.Exit(1)
	}

	logger.Infof("%s completed successfully. %s completed (%d cache hits).",
		goal,
		console.FCountTargets(successCount),
		cacheHits)
}
