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
	defer func() { _ = t.targetMutexMap.Unlock(target.Label.String()) }()

	if target.ChangeHash != "" {
		// ChangeHash already set
		return nil
	}

	dependencyHashes, err := t.dependencyHashes(target)
	if err != nil {
		return err
	}

	changeHash, err := GetTargetChangeHash(*target, dependencyHashes, t.extraArgs)
	if err != nil {
		return err
	}
	target.ChangeHash = changeHash
	return nil
}

// SetEnvironmentIdentityHash computes and sets the identity hash of an
// environment once all of its dependencies have completed.
func (t *TargetHasher) SetEnvironmentIdentityHash(environment *model.Environment) error {
	dependencyHashes, err := t.dependencyHashes(environment)
	if err != nil {
		return err
	}

	identityHash, err := GetEnvironmentIdentity(*environment, dependencyHashes)
	if err != nil {
		return err
	}
	environment.IdentityHash = identityHash
	return nil
}

// dependencyHashes returns the output hashes of the node's target
// dependencies and the identity hash of its environment.
func (t *TargetHasher) dependencyHashes(node model.BuildNode) ([]string, error) {
	dependencies := t.graph.GetDependencies(node)
	dependencyHashes := make([]string, 0, len(dependencies))
	for _, dependency := range dependencies {
		var dependencyHash string
		switch typedDependency := dependency.(type) {
		case *model.Target:
			dependencyHash = typedDependency.OutputHash
		case *model.Environment:
			dependencyHash = typedDependency.IdentityHash
		default:
			continue
		}

		if dependencyHash == "" {
			return nil, fmt.Errorf("dependency %s of %s has no output hash", dependency.GetLabel(), node.GetLabel())
		}
		dependencyHashes = append(dependencyHashes, dependencyHash)
	}
	return dependencyHashes, nil
}
