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
	// extraArgs are additional command-line arguments (from "--") that affect
	// the target command and must be included in the cache hash.
	extraArgs []string
}

func NewTargetHasher(graph *dag.DirectedTargetGraph) *TargetHasher {
	return &TargetHasher{
		graph:          graph,
		targetMutexMap: maps.NewMutexMap(),
	}
}

// SetExtraArgs configures additional command-line arguments that will be
// included in every target's definition hash.
func (t *TargetHasher) SetExtraArgs(args []string) {
	t.extraArgs = args
}

// SetTargetChangeHash computes and sets the target change hash.
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

	changeHash, err := GetTargetChangeHash(*target, dependencyHashes, t.extraArgs)
	if err != nil {
		return err
	}
	target.ChangeHash = changeHash
	return nil
}

// PriorChangeHashAtRef computes target's change hash as its input files existed
// at gitRef, for locating a layer-cache seed donor. It reuses each dependency's
// current OutputHash rather than recomputing the dependency graph at the ref:
// the result is used solely to find a donor image to warm `docker build`'s
// layer cache, so a drifted dependency or definition only misses the donor
// (cold build), never restores an incorrect result.
//
// gitRoot is the repository root used to resolve repo-relative input paths.
func (t *TargetHasher) PriorChangeHashAtRef(target *model.Target, gitRoot, gitRef string) (string, error) {
	t.targetMutexMap.Lock(target.Label.String())
	defer t.targetMutexMap.Unlock(target.Label.String())

	dependencies := t.graph.GetDependencies(target)
	dependencyHashes := make([]string, len(target.Dependencies))
	for index, dependency := range dependencies {
		targetDependency, ok := dependency.(*model.Target)
		if !ok {
			continue
		}
		dependencyHashes[index] = targetDependency.OutputHash
	}

	return GetTargetChangeHashAtRef(*target, dependencyHashes, t.extraArgs, gitRoot, gitRef)
}
