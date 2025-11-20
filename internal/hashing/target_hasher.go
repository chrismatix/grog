package hashing

import (
	"context"
	"grog/internal/dag"
	"grog/internal/maps"
	"grog/internal/model"
)

type OutputHashLoader interface {
	LoadTargetOutputHash(ctx context.Context, target *model.Target) (string, error)
}

type TargetHasher struct {
	graph            *dag.DirectedTargetGraph
	outputHashLoader OutputHashLoader
	// Ensure that we are only ever hashing one target at a time
	// to prevent race conditions
	targetMutexMap *maps.MutexMap
}

func NewTargetHasher(graph *dag.DirectedTargetGraph, loader OutputHashLoader) *TargetHasher {
	return &TargetHasher{
		graph:            graph,
		outputHashLoader: loader,
		targetMutexMap:   maps.NewMutexMap(),
	}
}

// SetTargetChangeHash computes and sets the target change hash which requires
// the change hashes of the direct dependencies and therefore recurses
func (t *TargetHasher) SetTargetChangeHash(ctx context.Context, target *model.Target) error {
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

		err := t.SetTargetChangeHash(ctx, targetDependency)
		if err != nil {
			return err
		}

		dependencyHashes[index] = targetDependency.ChangeHash
		if t.outputHashLoader != nil {
			if hash, err := t.outputHashLoader.LoadTargetOutputHash(ctx, targetDependency); err != nil {
				return err
			} else if hash != "" {
				dependencyHashes[index] = hash
			}
		}
	}

	changeHash, err := GetTargetChangeHash(*target, dependencyHashes)
	if err != nil {
		return err
	}
	target.ChangeHash = changeHash
	return nil
}
