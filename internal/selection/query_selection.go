package selection

import (
	"grog/internal/dag"
	"grog/internal/model"
)

// SelectTargets sets targets as selected
func (s *Selector) SelectTargets(
	graph *dag.DirectedTargetGraph,
) {
	for _, target := range graph.GetVertices() {
		if s.targetMatchesFilters(target) {
			target.IsSelected = true
		}
	}
}

func (s *Selector) FilterTargets(targets []*model.Target) []*model.Target {
	var filteredLabels []*model.Target
	for _, target := range targets {
		if s.targetMatchesFilters(target) {
			filteredLabels = append(filteredLabels, target)
		}
	}
	return filteredLabels
}
