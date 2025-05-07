package selection

import (
	"fmt"
	"grog/internal/config"
	"grog/internal/dag"
	"grog/internal/label"
	"grog/internal/model"
	"slices"
	"strings"
)

// SelectTargetsForBuild sets targets as selected
// returns the number of selected targets, the number of targets skipped due to platform mismatch
// and an error if a selected target depends on a target that does not match the platform
func (s *Selector) SelectTargetsForBuild(
	graph *dag.DirectedTargetGraph,
) (int, int, error) {
	selectedCountSum := 0
	platformSkippedSum := 0
	for _, pattern := range s.Patterns {
		selectedCount, platformSkipped, err := s.selectTargetsForPattern(graph, pattern)
		if err != nil {
			return 0, 0, err
		}
		selectedCountSum += selectedCount
		platformSkippedSum += platformSkipped
	}

	return selectedCountSum, platformSkippedSum, nil
}

func (s *Selector) selectTargetsForPattern(
	graph *dag.DirectedTargetGraph,
	pattern label.TargetPattern,
) (int, int, error) {

	platformSkipped := 0
	for _, target := range graph.GetVertices() {

		hasTag := false
		for _, tag := range s.Tags {
			if slices.Contains(target.Tags, tag) {
				hasTag = true
				break
			}
		}

		// Match pattern and test flag
		if pattern.Matches(target.Label) && targetMatchesTestSelection(target, s.TestFilter) && (hasTag || len(s.Tags) == 0) {
			if !targetMatchesPlatform(target) {
				platformSkipped += 1
				continue // Skip targets that don't match the platform
			}

			target.IsSelected = true
			if err := s.selectAllAncestors(graph, []string{target.Label.String()}, target); err != nil {
				return 0, 0, err
			}
		}
	}

	// Doing it all in one loop would be faster, but this is easier to reason about
	selectedCount := 0
	for _, target := range graph.GetVertices() {
		if target.IsSelected {
			selectedCount++
		}
	}

	return selectedCount, platformSkipped, nil
}

// selectAllAncestors recursively selects all ancestors of the given target
// and returns the number of selected targets.
func (s *Selector) selectAllAncestors(
	graph *dag.DirectedTargetGraph,
	depChain []string,
	target *model.Target,
) error {
	inEdges, err := graph.GetInEdges(target)
	if err != nil {
		return err
	}

	for _, ancestor := range inEdges {
		depChain = append(depChain, ancestor.Label.String())
		if !targetMatchesPlatform(ancestor) {
			depChainStr := strings.Join(depChain[1:], " -> ")
			return fmt.Errorf("could not select target %s because it depends on %s, which does not match the platform %s",
				depChain[0], depChainStr, config.Global.GetPlatform())
		}

		ancestor.IsSelected = true
		if err := s.selectAllAncestors(graph, depChain, ancestor); err != nil {
			return err
		}
	}
	return nil
}
