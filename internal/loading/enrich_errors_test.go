package loading

import (
	"strings"
	"testing"
)

func TestGetEnrichedPackage_InvalidDepLabel(t *testing.T) {
	logger := newTestLogger(t)
	dto := PackageDTO{
		SourceFilePath: "pkg/BUILD.json",
		Targets: []*TargetDTO{
			{Name: "t", Command: "echo", Dependencies: []string{"@@@invalid@@@"}},
		},
	}
	_, err := getEnrichedPackage(logger, "pkg", dto)
	if err == nil {
		t.Fatal("expected error for invalid dep label")
	}
}

func TestGetEnrichedPackage_DuplicateTarget(t *testing.T) {
	logger := newTestLogger(t)
	dto := PackageDTO{
		SourceFilePath: "pkg/BUILD.json",
		Targets: []*TargetDTO{
			{Name: "t", Command: "a"},
			{Name: "t", Command: "b"},
		},
	}
	_, err := getEnrichedPackage(logger, "pkg", dto)
	if err == nil || !strings.Contains(err.Error(), "duplicate target") {
		t.Fatalf("expected duplicate error, got %v", err)
	}
}

func TestGetEnrichedPackage_InvalidOutput(t *testing.T) {
	logger := newTestLogger(t)
	dto := PackageDTO{
		SourceFilePath: "pkg/BUILD.json",
		Targets: []*TargetDTO{
			{Name: "t", Command: "x", Outputs: []string{"unknown_type::foo"}},
		},
	}
	_, err := getEnrichedPackage(logger, "pkg", dto)
	if err == nil || !strings.Contains(err.Error(), "failed to parse outputs") {
		t.Fatalf("expected parse outputs error, got %v", err)
	}
}

func TestGetEnrichedPackage_InvalidBinOutput(t *testing.T) {
	logger := newTestLogger(t)
	dto := PackageDTO{
		SourceFilePath: "pkg/BUILD.json",
		Targets: []*TargetDTO{
			{Name: "t", Command: "x", BinOutput: "unknown_type::foo"},
		},
	}
	_, err := getEnrichedPackage(logger, "pkg", dto)
	if err == nil || !strings.Contains(err.Error(), "failed to parse bin output") {
		t.Fatalf("expected bin output error, got %v", err)
	}
}

func TestGetEnrichedPackage_BinOutputNotFile(t *testing.T) {
	logger := newTestLogger(t)
	dto := PackageDTO{
		SourceFilePath: "pkg/BUILD.json",
		Targets: []*TargetDTO{
			{Name: "t", Command: "x", BinOutput: "docker::foo"},
		},
	}
	_, err := getEnrichedPackage(logger, "pkg", dto)
	if err != nil && !strings.Contains(err.Error(), "must be of type file") && !strings.Contains(err.Error(), "failed to parse bin output") {
		t.Fatalf("expected bin output type error, got %v", err)
	}
}

func TestGetEnrichedPackage_InvalidTimeout(t *testing.T) {
	logger := newTestLogger(t)
	dto := PackageDTO{
		SourceFilePath: "pkg/BUILD.json",
		Targets: []*TargetDTO{
			{Name: "t", Command: "x", Timeout: "nope"},
		},
	}
	_, err := getEnrichedPackage(logger, "pkg", dto)
	if err == nil || !strings.Contains(err.Error(), "failed to parse timeout") {
		t.Fatalf("expected parse timeout error, got %v", err)
	}
}

func TestGetEnrichedPackage_ValidTimeout(t *testing.T) {
	logger := newTestLogger(t)
	dto := PackageDTO{
		SourceFilePath: "pkg/BUILD.json",
		Targets: []*TargetDTO{
			{Name: "t", Command: "x", Timeout: "30s"},
		},
	}
	pkg, err := getEnrichedPackage(logger, "pkg", dto)
	if err != nil {
		t.Fatal(err)
	}
	if len(pkg.Targets) != 1 {
		t.Fatalf("expected 1 target, got %d", len(pkg.Targets))
	}
}

func TestGetEnrichedPackage_RootPackage(t *testing.T) {
	logger := newTestLogger(t)
	dto := PackageDTO{
		SourceFilePath: "BUILD.json",
		Targets: []*TargetDTO{
			{Name: "t", Command: "x"},
		},
		Aliases: []*AliasDTO{
			{Name: "al", Actual: ":t"},
		},
	}
	pkg, err := getEnrichedPackage(logger, ".", dto)
	if err != nil {
		t.Fatal(err)
	}
	if pkg.Path != "" {
		t.Fatalf("expected empty path for root, got %q", pkg.Path)
	}
}

func TestGetEnrichedPackage_AliasInvalidActual(t *testing.T) {
	logger := newTestLogger(t)
	dto := PackageDTO{
		SourceFilePath: "pkg/BUILD.json",
		Aliases: []*AliasDTO{
			{Name: "al", Actual: "@@@invalid@@@"},
		},
	}
	_, err := getEnrichedPackage(logger, "pkg", dto)
	if err == nil {
		t.Fatal("expected error for invalid alias actual")
	}
}

func TestGetEnrichedPackage_AliasConflictsWithTarget(t *testing.T) {
	logger := newTestLogger(t)
	dto := PackageDTO{
		SourceFilePath: "pkg/BUILD.json",
		Targets:        []*TargetDTO{{Name: "shared", Command: "x"}},
		Aliases:        []*AliasDTO{{Name: "shared", Actual: ":shared"}},
	}
	_, err := getEnrichedPackage(logger, "pkg", dto)
	if err == nil || !strings.Contains(err.Error(), "duplicate") {
		t.Fatalf("expected duplicate error, got %v", err)
	}
}

func TestGetEnrichedPackage_DuplicateAlias(t *testing.T) {
	logger := newTestLogger(t)
	dto := PackageDTO{
		SourceFilePath: "pkg/BUILD.json",
		Targets:        []*TargetDTO{{Name: "t", Command: "x"}},
		Aliases: []*AliasDTO{
			{Name: "al", Actual: ":t"},
			{Name: "al", Actual: ":t"},
		},
	}
	_, err := getEnrichedPackage(logger, "pkg", dto)
	if err == nil || !strings.Contains(err.Error(), "duplicate") {
		t.Fatalf("expected duplicate alias error, got %v", err)
	}
}
