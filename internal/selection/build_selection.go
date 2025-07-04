package selection

import (
	"fmt"
	"grog/internal/config"
	"grog/internal/dag"
	"grog/internal/model"
	"strings"
)

/*

When selecting targets for a build/test/run we want to additionally check the platform
selectors when querying.

This could (should?) conceivably live in the analysis package so that queries and build share the same
target selection.
*/

// SelectTargetsForBuild sets targets as selected
// returns the number of selected targets, the number of targets skipped due to platform mismatch
// and an error if a selected target depends on a target that does not match the platform
func (s *Selector) SelectTargetsForBuild(
	graph *dag.DirectedTargetGraph,
) (int, int, error) {

	platformSkipped := 0
	for _, node := range graph.GetNodes() {
		// Match pattern and test flag
		if s.nodeMatchesFilters(node) {
			if !nodeMatchesPlatform(node) {
				platformSkipped += 1
				continue // Skip targets that don't match the platform
			}

			node.Select()
			if err := s.selectAllAncestorsForBuild(graph, []string{node.GetLabel().String()}, node); err != nil {
				return 0, 0, err
			}
		}
	}

	// Doing it all in one loop would be faster, but this is easier to reason about
	selectedCount := 0
	for _, node := range graph.GetNodes() {
		if node.GetIsSelected() {
			selectedCount++
		}
	}

	return selectedCount, platformSkipped, nil
}

// selectAllAncestorsForBuild recursively selects all ancestors of the given node
// and returns the number of selected targets.
func (s *Selector) selectAllAncestorsForBuild(
	graph *dag.DirectedTargetGraph,
	depChain []string,
	node model.BuildNode,
) error {
	for _, ancestor := range graph.GetDependencies(node) {
		depChain = append(depChain, ancestor.GetLabel().String())
		if !nodeMatchesPlatform(ancestor) {
			depChainStr := strings.Join(depChain[1:], " -> ")
			return fmt.Errorf("could not select node %s because it depends on %s, which does not match the platform %s",
				depChain[0], depChainStr, config.Global.GetPlatform())
		}

		ancestor.Select()
		if err := s.selectAllAncestorsForBuild(graph, depChain, ancestor); err != nil {
			return err
		}
	}
	return nil
}
