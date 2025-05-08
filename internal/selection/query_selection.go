package selection

import (
	"grog/internal/dag"
)

// SelectTargets sets targets as selected
func (s *Selector) SelectTargets(
	graph *dag.DirectedTargetGraph,
) error {
	for _, target := range graph.GetVertices() {
		if s.targetMatchesFilters(target) {
			target.IsSelected = true
		}
	}
	return nil
}
