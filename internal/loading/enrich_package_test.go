package loading

import (
	"go.uber.org/zap/zaptest"
	"grog/internal/label"
	"testing"
)

func TestGetEnrichedPackage_DefaultPlatforms(t *testing.T) {
	logger := zaptest.NewLogger(t).Sugar()
	packagePath := "test/package"

	// Create a package DTO with a default platform selector
	pkgDTO := PackageDTO{
		SourceFilePath:   "test/package/BUILD.json",
		DefaultPlatforms: []string{"linux/amd64"},
		Targets: []*TargetDTO{
			{
				// Target without platforms - should inherit default platforms
				Name:    "target1",
				Command: "echo 'target1'",
			},
			{
				// Target with its own platforms - should override default platforms
				Name:      "target2",
				Command:   "echo 'target2'",
				Platforms: []string{"darwin/arm64"},
			},
		},
	}

	enrichedPkg, err := getEnrichedPackage(logger, packagePath, pkgDTO)
	if err != nil {
		t.Fatalf("Failed to enrich package: %v", err)
	}

	// Check that target1 inherited the default platforms
	target1Label := "//test/package:target1"
	target1, ok := enrichedPkg.Targets[label.TL(packagePath, "target1")]
	if !ok {
		t.Fatalf("Target %s not found in enriched package", target1Label)
	}
	if target1.Platforms == nil {
		t.Fatalf("Target %s should have inherited the default platforms, but platforms is nil", target1Label)
	}
	if len(target1.Platforms) != 1 || target1.Platforms[0] != "linux/amd64" {
		t.Errorf("Target %s has incorrect platforms: got %v, want [linux/amd64]", target1Label, target1.Platforms)
	}

	// Check that target2 overrode the default platforms
	target2Label := "//test/package:target2"
	target2, ok := enrichedPkg.Targets[label.TL(packagePath, "target2")]
	if !ok {
		t.Fatalf("Target %s not found in enriched package", target2Label)
	}
	if target2.Platforms == nil {
		t.Fatalf("Target %s should have its own platforms, but platforms is nil", target2Label)
	}
	if len(target2.Platforms) != 1 || target2.Platforms[0] != "darwin/arm64" {
		t.Errorf("Target %s has incorrect platforms: got %v, want [darwin/arm64]", target2Label, target2.Platforms)
	}
}

func TestGetEnrichedPackage_FingerprintCopied(t *testing.T) {
	logger := zaptest.NewLogger(t).Sugar()
	packagePath := "pkg"

	fingerprint := map[string]string{"commit": "abc123", "arch": "arm64"}

	pkgDTO := PackageDTO{
		SourceFilePath: "pkg/BUILD.yaml",
		Targets: []*TargetDTO{
			{
				Name:        "example",
				Command:     "echo hi",
				Fingerprint: fingerprint,
			},
		},
	}

	enrichedPkg, err := getEnrichedPackage(logger, packagePath, pkgDTO)
	if err != nil {
		t.Fatalf("failed to enrich package: %v", err)
	}

	target := enrichedPkg.Targets[label.TL(packagePath, "example")]
	if target == nil {
		t.Fatalf("expected target to be enriched")
	}

	if len(target.Fingerprint) != len(fingerprint) {
		t.Fatalf("expected fingerprint to be copied, got %v", target.Fingerprint)
	}

	for k, v := range fingerprint {
		if target.Fingerprint[k] != v {
			t.Fatalf("expected fingerprint[%s] to be %s, got %s", k, v, target.Fingerprint[k])
		}
	}
}
