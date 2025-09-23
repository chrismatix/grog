package hashing

import (
	"testing"

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
