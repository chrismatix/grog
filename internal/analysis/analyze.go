package analysis

import (
	"fmt"
	"grog/internal/dag"
	"grog/internal/model"
)

// BuildGraph builds a directed graph of targets and analyzes it.
func BuildGraph(targets model.TargetMap) (*dag.DirectedTargetGraph, error) {
	nodeMap := model.BuildNodeMapFromTargets(targets.TargetsAlphabetically()...)
	graph := dag.NewDirectedGraphFromMap(nodeMap)

	// Add edges defined by dependencies
	for _, target := range targets {
		for _, depLabel := range target.Dependencies {
			dep := targets[depLabel]
			if dep == nil {
				return &dag.DirectedTargetGraph{}, fmt.Errorf("dependency %s of target %s not found", depLabel, target.Label)
			}

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
