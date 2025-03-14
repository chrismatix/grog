package model

// Package defines all the information that a package needs to build.
type Package struct {
	Targets []Target `json:"targets"`
}
