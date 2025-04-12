package model

import (
	"fmt"
	"grog/internal/label"
	"sort"
)

// TargetMap A map of targets by label
// this works because TargetLabel implements String()
type TargetMap map[label.TargetLabel]*Target

func TargetMapFromPackages(packages []*Package) (TargetMap, error) {
	targets := make(TargetMap)
	for _, pkg := range packages {
		for _, target := range pkg.GetTargets() {
			if _, ok := targets[target.Label]; ok {
				// This should never happen, but we check anyway
				return nil, fmt.Errorf("duplicate target label: %s", target.Label)
			}
			targets[target.Label] = target
		}
	}
	return targets, nil
}

func TargetMapFromTargets(targets ...*Target) TargetMap {
	targetMap := make(TargetMap)
	for _, target := range targets {
		targetMap[target.Label] = target
	}
	return targetMap
}

// TargetsAlphabetically returns the targets in alphabetical order
func (m TargetMap) TargetsAlphabetically() []*Target {
	var targets []*Target
	for _, target := range m {
		targets = append(targets, target)
	}
	sort.Slice(targets, func(i, j int) bool {
		return targets[i].Label.String() < targets[j].Label.String()
	})
	return targets
}
