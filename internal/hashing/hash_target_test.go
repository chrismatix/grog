package hashing

import (
	"testing"

	"grog/internal/config"
	"grog/internal/label"
	"grog/internal/model"
)

func TestHashTargetDefinition_FingerprintAffectsHash(t *testing.T) {
	target := model.Target{
		Label:       label.TL("pkg", "target"),
		Command:     "echo hi",
		Fingerprint: map[string]string{"version": "1.0.0"},
	}

	hashWithV1, err := hashTargetDefinition(target, nil)
	if err != nil {
		t.Fatalf("hashTargetDefinition returned error: %v", err)
	}

	target.Fingerprint["version"] = "1.0.1"

	hashWithV101, err := hashTargetDefinition(target, nil)
	if err != nil {
		t.Fatalf("hashTargetDefinition returned error: %v", err)
	}

	if hashWithV1 == hashWithV101 {
		t.Fatalf("expected hash to change when fingerprint value changes: %s", hashWithV101)
	}
}

func TestHashTargetDefinition_DockerBackendAffectsHashForDockerTargets(t *testing.T) {
	dockerTarget := model.Target{
		Label:   label.TL("pkg", "target"),
		Command: "docker build .",
		Outputs: []model.Output{model.NewOutput("docker", "my-image")},
	}

	config.Global.Docker.Backend = config.DockerBackendFS
	hashFS, err := hashTargetDefinition(dockerTarget, nil)
	if err != nil {
		t.Fatalf("hashTargetDefinition returned error: %v", err)
	}

	config.Global.Docker.Backend = config.DockerBackendRegistry
	hashRegistry, err := hashTargetDefinition(dockerTarget, nil)
	if err != nil {
		t.Fatalf("hashTargetDefinition returned error: %v", err)
	}

	if hashFS == hashRegistry {
		t.Fatalf("expected different hashes for different docker backends, got: %s", hashFS)
	}
}

func TestHashTargetDefinition_DockerBackendDoesNotAffectNonDockerTargets(t *testing.T) {
	target := model.Target{
		Label:   label.TL("pkg", "target"),
		Command: "echo hi",
		Outputs: []model.Output{model.NewOutput("file", "output.txt")},
	}

	config.Global.Docker.Backend = config.DockerBackendFS
	hashFS, err := hashTargetDefinition(target, nil)
	if err != nil {
		t.Fatalf("hashTargetDefinition returned error: %v", err)
	}

	config.Global.Docker.Backend = config.DockerBackendRegistry
	hashRegistry, err := hashTargetDefinition(target, nil)
	if err != nil {
		t.Fatalf("hashTargetDefinition returned error: %v", err)
	}

	if hashFS != hashRegistry {
		t.Fatalf("expected same hash for non-docker target regardless of docker backend, got: %s vs %s", hashFS, hashRegistry)
	}
}

func TestHashTargetDefinition_IgnoresUnrelatedFields(t *testing.T) {
	base := model.Target{
		Label:       label.TL("pkg", "target"),
		Command:     "echo hi",
		Fingerprint: map[string]string{"version": "1.0.0"},
	}

	hashBase, err := hashTargetDefinition(base, nil)
	if err != nil {
		t.Fatalf("hashTargetDefinition returned error: %v", err)
	}

	modified := base
	modified.Tags = append(modified.Tags, "new-tag")

	hashWithTags, err := hashTargetDefinition(modified, nil)
	if err != nil {
		t.Fatalf("hashTargetDefinition returned error: %v", err)
	}

	if hashBase != hashWithTags {
		t.Fatalf("expected hash to remain stable when unrelated fields change: %s vs %s", hashBase, hashWithTags)
	}
}
