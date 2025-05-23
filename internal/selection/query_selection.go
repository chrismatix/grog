package selection

import (
	"grog/internal/dag"
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
