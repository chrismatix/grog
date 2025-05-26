package loading

import (
	"go.uber.org/zap/zaptest"
	"grog/internal/label"
	"grog/internal/model"
	"testing"
)

func TestGetEnrichedPackage_DefaultPlatform(t *testing.T) {
	logger := zaptest.NewLogger(t).Sugar()
	packagePath := "test/package"

	// Create a package DTO with a default platform
	pkgDTO := PackageDTO{
		SourceFilePath: "test/package/BUILD.json",
		DefaultPlatform: &model.PlatformConfig{
			OS:   []string{"linux"},
			Arch: []string{"amd64"},
		},
		Targets: []*TargetDTO{
			{
				// Target without platform - should inherit default platform
				Name:    "target1",
				Command: "echo 'target1'",
			},
			{
				// Target with its own platform - should override default platform
				Name:    "target2",
				Command: "echo 'target2'",
				Platform: &model.PlatformConfig{
					OS:   []string{"darwin"},
					Arch: []string{"arm64"},
				},
			},
		},
	}

	enrichedPkg, err := getEnrichedPackage(logger, packagePath, pkgDTO)
	if err != nil {
		t.Fatalf("Failed to enrich package: %v", err)
	}

	// Check that target1 inherited the default platform
	target1Label := "//test/package:target1"
	target1, ok := enrichedPkg.Targets[label.TL(packagePath, "target1")]
	if !ok {
		t.Fatalf("Target %s not found in enriched package", target1Label)
	}
	if target1.Platform == nil {
		t.Fatalf("Target %s should have inherited the default platform, but platform is nil", target1Label)
	}
	if len(target1.Platform.OS) != 1 || target1.Platform.OS[0] != "linux" {
		t.Errorf("Target %s has incorrect OS platform: got %v, want [linux]", target1Label, target1.Platform.OS)
	}
	if len(target1.Platform.Arch) != 1 || target1.Platform.Arch[0] != "amd64" {
		t.Errorf("Target %s has incorrect Arch platform: got %v, want [amd64]", target1Label, target1.Platform.Arch)
	}

	// Check that target2 overrode the default platform
	target2Label := "//test/package:target2"
	target2, ok := enrichedPkg.Targets[label.TL(packagePath, "target2")]
	if !ok {
		t.Fatalf("Target %s not found in enriched package", target2Label)
	}
	if target2.Platform == nil {
		t.Fatalf("Target %s should have its own platform, but platform is nil", target2Label)
	}
	if len(target2.Platform.OS) != 1 || target2.Platform.OS[0] != "darwin" {
		t.Errorf("Target %s has incorrect OS platform: got %v, want [darwin]", target2Label, target2.Platform.OS)
	}
	if len(target2.Platform.Arch) != 1 || target2.Platform.Arch[0] != "arm64" {
		t.Errorf("Target %s has incorrect Arch platform: got %v, want [arm64]", target2Label, target2.Platform.Arch)
	}
}
