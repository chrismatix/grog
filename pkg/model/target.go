package model

import "grog/pkg/label"

// Target defines a build step.
type Target struct {
	// Label will be set during the loading stage so we prevent it from being serialized.
	Label label.TargetLabel `json:"-"`

	Name    string   `json:"name"`
	Deps    []string `json:"deps"`
	Inputs  []string `json:"inputs"`
	Outputs []string `json:"outputs"`
	Command string   `json:"cmd"`
}
