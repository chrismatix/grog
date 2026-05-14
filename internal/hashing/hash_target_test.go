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

	hashWithV1, err := hashTargetDefinition(target, nil, nil)
	if err != nil {
		t.Fatalf("hashTargetDefinition returned error: %v", err)
	}

	target.Fingerprint["version"] = "1.0.1"

	hashWithV101, err := hashTargetDefinition(target, nil, nil)
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
	hashFS, err := hashTargetDefinition(dockerTarget, nil, nil)
	if err != nil {
		t.Fatalf("hashTargetDefinition returned error: %v", err)
	}

	config.Global.Docker.Backend = config.DockerBackendRegistry
	hashRegistry, err := hashTargetDefinition(dockerTarget, nil, nil)
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
	hashFS, err := hashTargetDefinition(target, nil, nil)
	if err != nil {
		t.Fatalf("hashTargetDefinition returned error: %v", err)
	}

	config.Global.Docker.Backend = config.DockerBackendRegistry
	hashRegistry, err := hashTargetDefinition(target, nil, nil)
	if err != nil {
		t.Fatalf("hashTargetDefinition returned error: %v", err)
	}

	if hashFS != hashRegistry {
		t.Fatalf("expected same hash for non-docker target regardless of docker backend, got: %s vs %s", hashFS, hashRegistry)
	}
}

func TestHashTargetDefinition_PlatformTagsAffectHash(t *testing.T) {
	target := model.Target{
		Label:   label.TL("pkg", "target"),
		Command: "echo hi",
	}

	defer func() { config.Global.PlatformTags = nil }()

	config.Global.PlatformTags = nil
	hashEmpty, err := hashTargetDefinition(target, nil, nil)
	if err != nil {
		t.Fatalf("hashTargetDefinition returned error: %v", err)
	}

	config.Global.PlatformTags = []string{"qa-runner"}
	hashWithTag, err := hashTargetDefinition(target, nil, nil)
	if err != nil {
		t.Fatalf("hashTargetDefinition returned error: %v", err)
	}

	if hashEmpty == hashWithTag {
		t.Fatalf("expected hash to change when platform tags differ: %s", hashWithTag)
	}

	// Tag order should not matter.
	config.Global.PlatformTags = []string{"a", "b"}
	hashAB, err := hashTargetDefinition(target, nil, nil)
	if err != nil {
		t.Fatalf("hashTargetDefinition returned error: %v", err)
	}
	config.Global.PlatformTags = []string{"b", "a"}
	hashBA, err := hashTargetDefinition(target, nil, nil)
	if err != nil {
		t.Fatalf("hashTargetDefinition returned error: %v", err)
	}
	if hashAB != hashBA {
		t.Fatalf("expected platform tag order to be irrelevant for hashing: %s vs %s", hashAB, hashBA)
	}
}

func TestHashTargetDefinition_PlatformTagsIgnoredForMultiplatformCache(t *testing.T) {
	target := model.Target{
		Label:   label.TL("pkg", "target"),
		Command: "echo hi",
		Tags:    []string{model.TagMultiplatformCache},
	}

	defer func() { config.Global.PlatformTags = nil }()

	config.Global.PlatformTags = nil
	hashEmpty, err := hashTargetDefinition(target, nil, nil)
	if err != nil {
		t.Fatalf("hashTargetDefinition returned error: %v", err)
	}

	config.Global.PlatformTags = []string{"qa-runner"}
	hashWithTag, err := hashTargetDefinition(target, nil, nil)
	if err != nil {
		t.Fatalf("hashTargetDefinition returned error: %v", err)
	}

	if hashEmpty != hashWithTag {
		t.Fatalf("expected multiplatform-cache target hash to be stable across platform tags: %s vs %s", hashEmpty, hashWithTag)
	}
}

func TestHashTargetDefinition_IgnoresUnrelatedFields(t *testing.T) {
	base := model.Target{
		Label:       label.TL("pkg", "target"),
		Command:     "echo hi",
		Fingerprint: map[string]string{"version": "1.0.0"},
	}

	hashBase, err := hashTargetDefinition(base, nil, nil)
	if err != nil {
		t.Fatalf("hashTargetDefinition returned error: %v", err)
	}

	modified := base
	modified.Tags = append(modified.Tags, "new-tag")

	hashWithTags, err := hashTargetDefinition(modified, nil, nil)
	if err != nil {
		t.Fatalf("hashTargetDefinition returned error: %v", err)
	}

	if hashBase != hashWithTags {
		t.Fatalf("expected hash to remain stable when unrelated fields change: %s vs %s", hashBase, hashWithTags)
	}
}

func TestHashTargetDefinition_ExtraArgsAffectHash(t *testing.T) {
	target := model.Target{
		Label:   label.TL("pkg", "test"),
		Command: "pytest $@",
	}

	hashNoArgs, err := hashTargetDefinition(target, nil, nil)
	if err != nil {
		t.Fatalf("hashTargetDefinition returned error: %v", err)
	}

	hashWithArgs, err := hashTargetDefinition(target, nil, []string{"-k", "test_foo"})
	if err != nil {
		t.Fatalf("hashTargetDefinition returned error: %v", err)
	}

	if hashNoArgs == hashWithArgs {
		t.Fatalf("expected different hashes when extra args are provided, got same: %s", hashNoArgs)
	}

	// Different extra args should produce different hashes
	hashWithOtherArgs, err := hashTargetDefinition(target, nil, []string{"-k", "test_bar"})
	if err != nil {
		t.Fatalf("hashTargetDefinition returned error: %v", err)
	}

	if hashWithArgs == hashWithOtherArgs {
		t.Fatalf("expected different hashes for different extra args, got same: %s", hashWithArgs)
	}
}
