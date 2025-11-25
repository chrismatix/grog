package hashing

import (
	"fmt"
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

// SetTargetChangeHash computes and sets the target change hash
func (t *TargetHasher) SetTargetChangeHash(target *model.Target) error {
	t.targetMutexMap.Lock(target.Label.String())
	defer t.targetMutexMap.Unlock(target.Label.String())

	if target.ChangeHash != "" {
		// ChangeHash already set
		return nil
	}

	// Collect the OutputHash values of all dependencies
	dependencies := t.graph.GetDependencies(target)
	dependencyHashes := make([]string, len(target.Dependencies))
	for index, dependency := range dependencies {
		targetDependency, ok := dependency.(*model.Target)
		if !ok {
			// Only consider dependencies that are targets
			continue
		}

		outputHash := targetDependency.OutputHash
		if outputHash == "" {
			return fmt.Errorf("dependency %s of %s has no output hash", targetDependency.Label, target.Label)
		}

		dependencyHashes[index] = targetDependency.OutputHash
	}

	changeHash, err := GetTargetChangeHash(*target, dependencyHashes)
	if err != nil {
		return err
	}
	target.ChangeHash = changeHash
	return nil
}
