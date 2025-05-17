package loading

import (
	"context"
	"go.uber.org/zap"
	"grog/internal/analysis"
	"grog/internal/console"
	"grog/internal/dag"
	"grog/internal/model"
)

func MustLoadGraphForBuild(ctx context.Context, logger *zap.SugaredLogger) *dag.DirectedTargetGraph {
	packages, err := LoadPackages(ctx)
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

	logger.Infof("%s loaded, %s configured.", console.FCountPkg(len(packages)), console.FCountTargets(len(targets)))
	return graph
}

func MustLoadGraphForQuery(ctx context.Context, logger *zap.SugaredLogger) *dag.DirectedTargetGraph {
	packages, err := LoadPackages(ctx)
	if err != nil {
		logger.Fatalf("could not load packages: %v", err)
	}

	targets, err := model.TargetMapFromPackages(packages)
	if err != nil {
		logger.Fatalf("could not create target map: %v", err)
	}

	graph, err := analysis.BuildGraph(targets)
	if err != nil {
		logger.Fatalf("could not build graph: %v", err)
	}

	return graph
}
