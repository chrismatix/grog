package model

import (
	"grog/internal/label"
	"strings"
)

// Target defines a build step that depends on Deps (other targets)
// and Inputs (files) and produces Outputs.
type Target struct {
	Label label.TargetLabel `json:"label"`

	Command string              `json:"cmd"`
	Deps    []label.TargetLabel `json:"deps,omitempty"`
	Inputs  []string            `json:"inputs,omitempty"`
	Outputs []string            `json:"outputs,omitempty"`

	// Whether this target is selected for execution.
	IsSelected bool `json:"is_selected,omitempty"`

	// ChangeHash is the combined hash of the target definition and its input files
	ChangeHash  string `json:"change_hash,omitempty"`
	HasCacheHit bool   `json:"has_cache_hit,omitempty"`
}

func (t *Target) GetDepsString() []string {
	stringDeps := make([]string, len(t.Deps))
	for i, dep := range t.Deps {
		stringDeps[i] = dep.String()
	}
	return stringDeps
}

func (t *Target) CommandEllipsis() string {
	firstLine := strings.SplitN(t.Command, "\n", 2)[0]
	if len(firstLine) > 70 {
		return firstLine[:67] + "..."
	}
	return firstLine
}

func (t *Target) IsTest() bool {
	return t.Label.IsTest()
}
