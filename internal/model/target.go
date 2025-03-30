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

	// Hashes used for detecting cache hits
	// Loaded* refers to what we found in the cache vs what we computed
	// Hash the Deps, Inputs, Outputs, and Command
	CachedDefinitionHash   string
	ComputedDefinitionHash string

	// Hash the actual contents of Inputs
	CachedInputContentHash   string
	ComputedInputContentHash string
}

func (t *Target) CommandEllipsis() string {
	if len(t.Command) > 70 {
		return t.Command[:67] + "..."
	}
	return t.Command
}

func (t *Target) IsTest() bool {
	return t.Label.IsTest()
}
