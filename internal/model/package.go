package model

import (
	"grog/internal/label"
	"maps"
	"slices"
)

// Package defines all the information that a package needs to build.
type Package struct {
	DependencyResolvers map[label.TargetLabel]*DependencyResolver `json:"dependency_resolvers,omitempty"`
	// Record the path to this package relative to the workspace root
	Path string

	Targets   map[label.TargetLabel]*Target   `json:"targets"`
	Aliases   map[label.TargetLabel]*Alias    `json:"aliases"`
	Resources map[label.TargetLabel]*Resource `json:"resources"`
}

func (p *Package) GetTargets() []*Target {
	return slices.Collect(maps.Values(p.Targets))
}

func (p *Package) GetAliases() []*Alias {
	return slices.Collect(maps.Values(p.Aliases))
}

func (p *Package) GetResources() []*Resource {
	return slices.Collect(maps.Values(p.Resources))
}
