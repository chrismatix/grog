package model

import "grog/pkg/label"

// Target defines a build step.
type Target struct {
	// Label will be set during the loading stage so we prevent it from being serialized.
	Label label.TargetLabel `json:"-"`
	// Name will be set based on the key of this target in the package target map.
	Name string `json:"-"`

	Command string   `json:"cmd"`
	Deps    []string `json:"deps,omitempty"`
	Inputs  []string `json:"inputs,omitempty"`
	Outputs []string `json:"outputs,omitempty"`
}
