package analysis

import (
	"fmt"
	"grog/internal/dag"
	"grog/internal/model"
)

// BuildGraph builds a directed graph of targets and analyzes it.
func BuildGraph(targets model.TargetMap) (*dag.DirectedTargetGraph, error) {
	graph := dag.NewDirectedGraphFromMap(targets)

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

	if graph.HasCycle() {
		return &dag.DirectedTargetGraph{}, fmt.Errorf("cycle detected")
	}

	return graph, nil
}
