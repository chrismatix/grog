package cmds

import (
	"context"
	"errors"
	"github.com/fatih/color"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"grog/internal/analysis"
	"grog/internal/console"
	"grog/internal/execution"
	"grog/internal/label"
	"grog/internal/loading"
	"grog/internal/model"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"
)

var BuildCmd = &cobra.Command{
	Use:   "build",
	Short: "Loads the user configuration and executes build targets",
	Long:  `Loads the user configuration, checks which targets need to be rebuilt based on file hashes, builds the dependency graph, and executes targets.`,
	Args:  cobra.MaximumNArgs(1), // Optional argument for target pattern
	Run: func(cmd *cobra.Command, args []string) {
		logger := console.InitLogger()
		if len(args) > 0 {
			targetPattern, err := label.ParseTargetPattern(args[0])
			if err != nil {
				logger.Fatalf("could not parse target pattern: %v", err)
			}
			runBuild(
				targetPattern,
				true,
				false)
		} else {
			// No target pattern: build all targets
			runBuild(
				label.TargetPattern{},
				false,
				false,
			)
		}
	},
}

// runBuild runs the build/test command with the given target pattern
func runBuild(targetPattern label.TargetPattern, hasTargetPattern bool, isTest bool) {
	startTime := time.Now()
	logger := console.InitLogger()

	packages, err := loading.LoadPackages()
	if err != nil {
		logger.Fatalf(
			"could not load packages: %v",
			err)
	}

	numPackages := len(packages)
	targets, err := model.TargetMapFromPackages(packages)
	if err != nil {
		logger.Fatalf("could not create target map: %v", err)
	}

	graph, err := analysis.BuildGraphAndAnalyze(targets)
	if err != nil {
		logger.Fatalf("could not build graph: %v", err)
	}

	if hasTargetPattern {
		selectedCount := graph.SelectTargets(targetPattern, isTest)
		if selectedCount == 0 {
			logger.Fatalf("could not find any targets matching %s", targetPattern.String())
		}

		logger.Infof("Selected %s (%s loaded, %s configured).",
			console.FCountTargets(selectedCount),
			console.FCountPkg(numPackages),
			console.FCountTargets(len(targets)))
	} else {
		// No target pattern: build all targets
		graph.SelectAllTargets()
		logger.Infof("Selected all targets (%s loaded, %s configured).", console.FCountPkg(numPackages), console.FCountTargets(len(targets)))
	}

	ctx, cancel := context.WithCancel(context.Background())
	ctx = console.SetLogger(ctx, logger)
	defer cancel()

	// Listen for SIGTERM or SIGINT to cancel the context
	signalChan := make(chan os.Signal, 1)
	signal.Notify(signalChan, os.Interrupt, syscall.SIGTERM)
	go func() {
		select {
		case <-signalChan:
			cancel()
		case <-ctx.Done():
		}
	}()

	failFast := viper.GetBool("fail_fast")
	err, completionMap := execution.Execute(ctx, graph, failFast)

	elapsedTime := time.Since(startTime).Seconds()
	// Mostly used to keep our test fixtures deterministic
	logExecutionTime := viper.GetBool("disable_time_logging")
	if !logExecutionTime {
		logger.Infof("Elapsed time: %.3fs", elapsedTime)
	}

	if err != nil {
		graph.LogGraphJSON(logger)
		logger.Errorf("execution failed: %v", err)
		// exit
		os.Exit(1)
	}

	buildErrors := completionMap.GetErrors()
	successes := completionMap.GetSuccesses()
	if len(buildErrors) > 0 {
		logger.Errorf("Build failed. %s completed, %d failed:", console.FCountTargets(len(successes)), len(buildErrors))
		for target, completion := range completionMap {
			if completion.IsSuccess {
				continue
			}

			var executionError *execution.CommandError
			color.Red("---------------------------------")
			if completion.Err == nil {
				logger.Errorf("Target %s failed with no error", target.Label)
			} else if errors.As(completion.Err, &executionError) {
				logger.Errorf("Target %s failed with exit code %d:\ncmd: \"%s\"\n%s",
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

	logger.Infof("Build completed successfully. %s completed.", console.FCountTargets(len(successes)))
	os.Exit(0)
}
