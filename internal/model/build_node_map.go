package model

import (
	"fmt"
	"grog/internal/label"
	"maps"
	"slices"
	"sort"
)

type BuildNodeMap map[label.TargetLabel]BuildNode

func BuildNodeMapFromPackages(packages []*Package) (BuildNodeMap, error) {
	nodes := make(BuildNodeMap)
	for _, pkg := range packages {
		for _, t := range pkg.GetTargets() {
			if _, ok := nodes[t.Label]; ok {
				return nil, fmt.Errorf("duplicate target label: %s", t.Label)
			}
			nodes[t.Label] = t
		}
		for _, env := range pkg.GetEnvironments() {
			if _, ok := nodes[env.Label]; ok {
				return nil, fmt.Errorf("duplicate target label: %s", env.Label)
			}
			nodes[env.Label] = env
		}
	}
	return nodes, nil
}

// BuildNodeMapFromTargets constructs a BuildNodeMap from the provided targets.
func BuildNodeMapFromTargets(targets ...*Target) BuildNodeMap {
	nodes := make(BuildNodeMap)
	for _, t := range targets {
		nodes[t.Label] = t
	}
	return nodes
}

func (m BuildNodeMap) NodesAlphabetically() []BuildNode {
	nodes := slices.Collect(maps.Values(m))
	sort.Slice(nodes, func(i, j int) bool {
		return nodes[i].GetLabel().String() < nodes[j].GetLabel().String()
	})
	return nodes
}
