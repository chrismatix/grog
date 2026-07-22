package loading

import (
	"grog/internal/console"
	"grog/internal/label"
	"testing"

	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest"
)

func TestGetEnrichedPackage_DefaultPlatforms(t *testing.T) {
	logger := console.NewFromSugared(zaptest.NewLogger(t).Sugar(), zapcore.DebugLevel)
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
	logger := console.NewFromSugared(zaptest.NewLogger(t).Sugar(), zapcore.DebugLevel)
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

func TestGetEnrichedPackage_Resources(t *testing.T) {
	logger := console.NewFromSugared(zaptest.NewLogger(t).Sugar(), zapcore.DebugLevel)
	packagePath := "test/package"

	pkgDTO := PackageDTO{
		SourceFilePath: "test/package/BUILD.yaml",
		Targets: []*TargetDTO{
			{Name: "image", Command: "echo image"},
		},
		Resources: []*ResourceDTO{
			{
				Name:         "postgres",
				Up:           "docker run -d postgres",
				Down:         "docker rm -f postgres",
				Ready:        "pg_isready",
				Timeout:      "2m",
				Exports:      map[string]string{"DATABASE_URL": "postgres://localhost/test"},
				Dependencies: []string{":image"},
			},
		},
	}

	enrichedPkg, err := getEnrichedPackage(logger, packagePath, pkgDTO)
	if err != nil {
		t.Fatalf("Failed to enrich package: %v", err)
	}

	resource, ok := enrichedPkg.Resources[label.TL(packagePath, "postgres")]
	if !ok {
		t.Fatalf("Resource //test/package:postgres not found in enriched package")
	}
	if resource.Up != "docker run -d postgres" || resource.Down != "docker rm -f postgres" || resource.Ready != "pg_isready" {
		t.Errorf("Resource lifecycle commands not preserved: %+v", resource)
	}
	if resource.Timeout.String() != "2m0s" {
		t.Errorf("Resource timeout not parsed: got %s", resource.Timeout)
	}
	if resource.Exports["DATABASE_URL"] != "postgres://localhost/test" {
		t.Errorf("Resource exports not preserved: %v", resource.Exports)
	}
	if len(resource.Dependencies) != 1 || resource.Dependencies[0].String() != "//test/package:image" {
		t.Errorf("Resource dependencies not parsed: %v", resource.Dependencies)
	}
}

func TestGetEnrichedPackage_ResourceRequiresUp(t *testing.T) {
	logger := console.NewFromSugared(zaptest.NewLogger(t).Sugar(), zapcore.DebugLevel)

	pkgDTO := PackageDTO{
		SourceFilePath: "test/package/BUILD.yaml",
		Resources:      []*ResourceDTO{{Name: "postgres"}},
	}

	if _, err := getEnrichedPackage(logger, "test/package", pkgDTO); err == nil {
		t.Fatal("expected error for resource without up command")
	}
}

func TestGetEnrichedPackage_ResourceDuplicateLabel(t *testing.T) {
	logger := console.NewFromSugared(zaptest.NewLogger(t).Sugar(), zapcore.DebugLevel)

	pkgDTO := PackageDTO{
		SourceFilePath: "test/package/BUILD.yaml",
		Targets:        []*TargetDTO{{Name: "postgres", Command: "echo hi"}},
		Resources:      []*ResourceDTO{{Name: "postgres", Up: "true"}},
	}

	if _, err := getEnrichedPackage(logger, "test/package", pkgDTO); err == nil {
		t.Fatal("expected duplicate label error")
	}
}
