package hashing

import (
	"testing"

	"grog/internal/dag"
	"grog/internal/label"
	"grog/internal/model"
)

func TestTargetHasherIgnoresResourceDependencies(t *testing.T) {
	dependency := &model.Target{
		Label:      label.TL("pkg", "dependency"),
		OutputHash: "dependency-output",
	}
	resource := &model.Resource{Label: label.TL("pkg", "database"), Up: "true"}
	withoutResource := &model.Target{
		Label:        label.TL("pkg", "consumer"),
		Command:      "true",
		Dependencies: []label.TargetLabel{dependency.Label},
	}
	withResource := &model.Target{
		Label:        withoutResource.Label,
		Command:      withoutResource.Command,
		Dependencies: []label.TargetLabel{dependency.Label, resource.Label},
	}

	withoutResourceGraph := dag.NewDirectedGraphFromTargets(dependency, withoutResource)
	if err := withoutResourceGraph.AddEdge(dependency, withoutResource); err != nil {
		t.Fatalf("failed to add dependency edge: %v", err)
	}
	if err := NewTargetHasher(withoutResourceGraph).SetTargetChangeHash(withoutResource); err != nil {
		t.Fatalf("failed to hash target without resource: %v", err)
	}

	withResourceGraph := dag.NewDirectedGraphFromTargets(dependency, resource, withResource)
	if err := withResourceGraph.AddEdge(dependency, withResource); err != nil {
		t.Fatalf("failed to add dependency edge: %v", err)
	}
	if err := withResourceGraph.AddEdge(resource, withResource); err != nil {
		t.Fatalf("failed to add resource edge: %v", err)
	}
	if err := NewTargetHasher(withResourceGraph).SetTargetChangeHash(withResource); err != nil {
		t.Fatalf("failed to hash target with resource: %v", err)
	}

	if withoutResource.ChangeHash != withResource.ChangeHash {
		t.Fatalf("resource changed target hash: %s != %s", withoutResource.ChangeHash, withResource.ChangeHash)
	}
}
