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
		targetDependency, ok := dependency.(*model.Target)
		if !ok {
			// Only consider dependencies that are targets
			continue
		}

		err := t.SetTargetChangeHash(targetDependency)
		if err != nil {
			return err
		}
		dependencyHashes[index] = targetDependency.ChangeHash
	}

	changeHash, err := GetTargetChangeHash(*target, dependencyHashes)
	if err != nil {
		return err
	}
	target.ChangeHash = changeHash
	return nil
}
