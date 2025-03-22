package model

import "grog/internal/label"

// Target defines a build step that depends on Deps (other targets)
// and Inputs (files) and produces Outputs.
type Target struct {
	Label label.TargetLabel

	Command string
	Deps    []label.TargetLabel
	Inputs  []string
	Outputs []string

	// Whether this target is selected for execution.
	IsSelected bool
	// Whether, or not the file inputs changed
	InputsChanged bool
}

func (t *Target) IsTest() bool {
	return t.Label.IsTest()
}
