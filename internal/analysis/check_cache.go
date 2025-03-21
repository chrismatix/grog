package analysis

import "grog/internal/dag"

// CheckInputsForSelected checks whether the Inputs of the selected targets have changed.
// and sets the InputsChanged flag on the targets.
// Returns the number of targets that have changed.
func CheckInputsForSelected(graph *dag.DirectedTargetGraph) error {
	// TODO use the same graph walking that we want to use for execution
	return nil
}
