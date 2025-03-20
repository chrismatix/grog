package model

import (
	"fmt"
	"grog/internal/label"
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
