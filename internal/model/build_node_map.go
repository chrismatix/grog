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
	}
	return nodes, nil
}

// BuildNodeMapFromNodes constructs a BuildNodeMap from the provided targets.
func BuildNodeMapFromNodes(nodes ...BuildNode) BuildNodeMap {
	nodeMap := make(BuildNodeMap)
	for _, node := range nodes {
		nodeMap[node.GetLabel()] = node
	}
	return nodeMap
}

func (m BuildNodeMap) GetTargets() []*Target {
	var targets []*Target
	for _, node := range m {
		if target, ok := node.(*Target); ok {
			targets = append(targets, target)
		}
	}
	return targets
}

func (m BuildNodeMap) NodesAlphabetically() []BuildNode {
	nodes := slices.Collect(maps.Values(m))
	sort.Slice(nodes, func(i, j int) bool {
		return nodes[i].GetLabel().String() < nodes[j].GetLabel().String()
	})
	return nodes
}

// SelectedNodesAlphabetically returns the selected nodes in alphabetical order
func (m BuildNodeMap) SelectedNodesAlphabetically() []BuildNode {
	var nodes []BuildNode
	for _, node := range m {
		if node.GetIsSelected() {
			nodes = append(nodes, node)
		}
	}
	sort.Slice(nodes, func(i, j int) bool {
		return nodes[i].GetLabel().String() < nodes[j].GetLabel().String()
	})
	return nodes
}
