package hashing

import (
	"grog/internal/dag"
	"grog/internal/maps"
	"grog/internal/model"
)

type TargetHasher struct {
	graph *dag.DirectedTargetGraph
	// Ensure that we are only ever hashing one target at a time
	// to prevent race conditions
	targetMutexMap *maps.MutexMap
}

func NewTargetHasher(graph *dag.DirectedTargetGraph) *TargetHasher {
	return &TargetHasher{
		graph:          graph,
		targetMutexMap: maps.NewMutexMap(),
	}
}

// SetTargetChangeHash computes and sets the target change hash which requires
// the change hashes of the direct dependencies and therefore recurses
func (t *TargetHasher) SetTargetChangeHash(target *model.Target) error {
	t.targetMutexMap.Lock(target.Label.String())
	defer t.targetMutexMap.Unlock(target.Label.String())

	if target.ChangeHash != "" {
		return nil
	}

	dependencies := t.graph.GetDependencies(target)
	dependencyHashes := make([]string, len(target.Dependencies))
	for index, dependency := range dependencies {
		err := t.SetTargetChangeHash(dependency)
		if err != nil {
			return err
		}
		dependencyHashes[index] = dependency.ChangeHash
	}

	changeHash, err := GetTargetChangeHash(*target, dependencyHashes)
	if err != nil {
		return err
	}
	target.ChangeHash = changeHash
	return nil
}
