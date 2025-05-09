package model

import (
	"grog/internal/label"
	"maps"
	"slices"
)

// Package defines all the information that a package needs to build.
type Package struct {
	// Record the path to the source file that defines this package.
	// for logging purposes
	SourceFilePath string

	Targets map[label.TargetLabel]*Target `json:"targets"`
}

func (p *Package) GetTargets() []*Target {
	return slices.Collect(maps.Values(p.Targets))
}
