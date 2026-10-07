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

func TestTargetHasherIncludesEnvironmentIdentity(t *testing.T) {
	hashTarget := func(imageOutputHash string, configuration map[string]string) string {
		t.Helper()
		image := &model.Target{Label: label.TL("envs", "image"), OutputHash: imageOutputHash}
		environment := &model.Environment{
			Label:        label.TL("envs", "linux"),
			Provider:     model.DockerProvider,
			Config:       configuration,
			Dependencies: []label.TargetLabel{image.Label},
		}
		target := &model.Target{
			Label:       label.TL("app", "server"),
			Command:     "make",
			Environment: &environment.Label,
		}

		graph := dag.NewDirectedGraphFromTargets(image, environment, target)
		if err := graph.AddEdge(image, environment); err != nil {
			t.Fatalf("failed to add image edge: %v", err)
		}
		if err := graph.AddEdge(environment, target); err != nil {
			t.Fatalf("failed to add environment edge: %v", err)
		}

		hasher := NewTargetHasher(graph)
		if err := hasher.SetTargetChangeHash(target); err == nil {
			t.Fatalf("expected an error while the environment has no identity hash")
		}
		if err := hasher.SetEnvironmentIdentityHash(environment); err != nil {
			t.Fatalf("failed to hash environment: %v", err)
		}
		if err := hasher.SetTargetChangeHash(target); err != nil {
			t.Fatalf("failed to hash target: %v", err)
		}
		return target.ChangeHash
	}

	baseline := hashTarget("image-v1", map[string]string{"image": "builder:1"})
	if baseline != hashTarget("image-v1", map[string]string{"image": "builder:1"}) {
		t.Fatalf("expected a stable change hash")
	}
	if baseline == hashTarget("image-v2", map[string]string{"image": "builder:1"}) {
		t.Errorf("rebuilding the environment's image must change the target hash")
	}
	if baseline == hashTarget("image-v1", map[string]string{"image": "builder:2"}) {
		t.Errorf("changing the environment config must change the target hash")
	}
}
