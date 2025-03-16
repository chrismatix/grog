package model

import "grog/pkg/label"

// Target defines a build step.
type Target struct {
	// Label will be set during the loading stage so we prevent it from being serialized.
	Label label.TargetLabel `json:"-"`

	Name    string   `json:"name"`
	Command string   `json:"cmd"`
	Deps    []string `json:"deps,omitempty"`
	Inputs  []string `json:"inputs,omitempty"`
	Outputs []string `json:"outputs,omitempty"`
}
