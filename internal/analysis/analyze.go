package analysis

import (
	"fmt"
	"grog/internal/dag"
	"grog/internal/model"
)

// BuildGraphAndAnalyze builds a directed graph of targets and analyzes it.
func BuildGraphAndAnalyze(targets model.TargetMap) (*dag.DirectedTargetGraph, error) {
	g, err := buildGraph(targets)
	if err != nil {
		return &dag.DirectedTargetGraph{}, err
	}

	if g.HasCycle() {
		return &dag.DirectedTargetGraph{}, fmt.Errorf("cycle detected")
	}

	return g, nil
}

func buildGraph(targets model.TargetMap) (*dag.DirectedTargetGraph, error) {
	graph := dag.NewDirectedGraphFromVertices(targets)

	// Add edges defined by dependencies
	for _, target := range targets {
		for _, depLabel := range target.Deps {
			dep := targets[depLabel]

			err := graph.AddEdge(dep, target)
			if err != nil {
				return &dag.DirectedTargetGraph{}, err
			}
		}
	}

	return graph, nil
}
