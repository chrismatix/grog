package loading

import (
	"context"
	"time"

	"grog/internal/analysis"
	"grog/internal/config"
	"grog/internal/console"
	"grog/internal/dag"
	"grog/internal/model"
)

func MustLoadGraphForBuild(ctx context.Context, logger *console.Logger) *dag.DirectedTargetGraph {
	startTime := time.Now()
	packages, err := LoadAllPackages(ctx)
	if err != nil {
		logger.Fatalf(
			"could not load packages: %v",
			err)
	}

	nodes, err := model.BuildNodeMapFromPackages(packages)
	if err != nil {
		logger.Fatalf("could not create target map: %v", err)
	}

	graph, err := analysis.BuildGraph(nodes)
	if err != nil {
		logger.Fatalf("could not build graph: %v", err)
	}

	if config.Global.DisableNonDeterministicLogging {
		logger.Infof("%s loaded, %s configured.",
			console.FCountPkg(len(packages)),
			console.FCountTargets(len(nodes.GetTargets())))
	} else {
		elapsedTime := time.Since(startTime).Seconds()
		logger.Infof("%s loaded in %.2fs, %s configured.",
			console.FCountPkg(len(packages)),
			elapsedTime,
			console.FCountTargets(len(nodes.GetTargets())),
		)
	}
	return graph
}

func MustLoadGraphForQuery(ctx context.Context, logger *console.Logger) *dag.DirectedTargetGraph {
	packages, err := LoadAllPackages(ctx)
	if err != nil {
		logger.Fatalf("could not load packages: %v", err)
	}

	nodes, err := model.BuildNodeMapFromPackages(packages)
	if err != nil {
		logger.Fatalf("could not create target map: %v", err)
	}

	graph, err := analysis.BuildGraph(nodes)
	if err != nil {
		logger.Fatalf("could not build graph: %v", err)
	}

	return graph
}
