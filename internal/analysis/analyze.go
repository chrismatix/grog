package analysis

import (
	"fmt"
	"grog/internal/dag"
	"grog/internal/model"
)

// BuildGraph builds a directed graph of targets and analyzes it.
func BuildGraph(nodes model.BuildNodeMap) (*dag.DirectedTargetGraph, error) {
	graph := dag.NewDirectedGraphFromMap(nodes)

	// Add edges defined by dependencies
	for _, node := range nodes {
		for _, depLabel := range node.GetDependencies() {
			dep := nodes[depLabel]
			if dep == nil {
				return &dag.DirectedTargetGraph{}, fmt.Errorf("dependency %s of node %s not found", depLabel, node.GetLabel())
			}

			err := graph.AddEdge(dep, node)
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
