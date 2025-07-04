package selection

import (
	"grog/internal/dag"
	"grog/internal/model"
)

// SelectTargets sets targets as selected
func (s *Selector) SelectTargets(
	graph *dag.DirectedTargetGraph,
) {
	for _, node := range graph.GetNodes() {
		if s.nodeMatchesFilters(node) && nodeMatchesPlatform(node) {
			node.Select()
		}
	}
}

func (s *Selector) FilterNodes(nodes []model.BuildNode) []model.BuildNode {
	var filteredLabels []model.BuildNode
	for _, node := range nodes {
		if s.Match(node) {
			filteredLabels = append(filteredLabels, node)
		}
	}
	return filteredLabels
}

func (s *Selector) Match(node model.BuildNode) bool {
	return s.nodeMatchesFilters(node) && nodeMatchesPlatform(node)
}
