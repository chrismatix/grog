package model

// Target defines a build step.
type Target struct {
	Name    string   `json:"name"`
	Deps    []string `json:"deps"`
	Inputs  []string `json:"inputs"`
	Outputs []string `json:"outputs"`
	Command string   `json:"cmd"`
}
