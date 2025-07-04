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
	Aliases map[label.TargetLabel]*Alias  `json:"aliases"`
}

func (p *Package) GetTargets() []*Target {
	return slices.Collect(maps.Values(p.Targets))
}

func (p *Package) GetAliases() []*Alias {
	return slices.Collect(maps.Values(p.Aliases))
}
