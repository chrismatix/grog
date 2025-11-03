package analysis

import (
	"fmt"
	"grog/internal/dag"
	"grog/internal/model"
	"strings"
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

	if cycle, hasCycle := graph.FindCycle(); hasCycle {
		var chain []string
		for _, node := range cycle {
			chain = append(chain, node.GetLabel().String())
		}
		return &dag.DirectedTargetGraph{}, fmt.Errorf("cycle detected: %s", strings.Join(chain, " -> "))
	}

	return graph, nil
}
