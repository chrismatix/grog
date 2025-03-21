package analysis

import (
	"fmt"
	"grog/internal/dag"
	"grog/internal/model"
)

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
	graph := dag.NewDirectedGraph()

	// Add all vertices
	for _, target := range targets {
		graph.AddVertex(target)
	}
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
