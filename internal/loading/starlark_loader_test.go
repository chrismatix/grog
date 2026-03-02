package loading

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"grog/internal/config"
)

func TestStarlarkLoader_CycleDetection(t *testing.T) {
	// Create a temporary directory for test files
	tmpDir := t.TempDir()

	// Set workspace root
	oldWorkspaceRoot := config.Global.WorkspaceRoot
	config.Global.WorkspaceRoot = tmpDir
	defer func() {
		config.Global.WorkspaceRoot = oldWorkspaceRoot
	}()

	// Create two files that load each other (cycle)
	cycleA := filepath.Join(tmpDir, "cycle_a.star")
	cycleB := filepath.Join(tmpDir, "cycle_b.star")
	buildFile := filepath.Join(tmpDir, "BUILD.star")

	err := os.WriteFile(cycleA, []byte(`load("//cycle_b.star", "func_b")
def func_a():
    return "a"
`), 0644)
	if err != nil {
		t.Fatal(err)
	}

	err = os.WriteFile(cycleB, []byte(`load("//cycle_a.star", "func_a")
def func_b():
    return "b"
`), 0644)
	if err != nil {
		t.Fatal(err)
	}

	err = os.WriteFile(buildFile, []byte(`load("//cycle_a.star", "func_a")
target(name = "test", command = "echo test")
`), 0644)
	if err != nil {
		t.Fatal(err)
	}

	// Try to load the BUILD file that triggers the cycle
	loader := StarlarkLoader{}
	_, _, err = loader.Load(context.Background(), buildFile)

	// Should get a cycle detection error
	if err == nil {
		t.Fatal("expected cycle detection error, got nil")
	}

	if !strings.Contains(err.Error(), "cycle detected") {
		t.Fatalf("expected 'cycle detected' in error, got: %v", err)
	}
}

func TestStarlarkLoader_ModuleCaching(t *testing.T) {
	// Create a temporary directory for test files
	tmpDir := t.TempDir()

	// Set workspace root
	oldWorkspaceRoot := config.Global.WorkspaceRoot
	config.Global.WorkspaceRoot = tmpDir
	defer func() {
		config.Global.WorkspaceRoot = oldWorkspaceRoot
	}()

	// Create a shared module
	sharedModule := filepath.Join(tmpDir, "shared.star")
	err := os.WriteFile(sharedModule, []byte(`def shared_func(name):
    target(name = name, command = "echo " + name)
`), 0644)
	if err != nil {
		t.Fatal(err)
	}

	// Create two BUILD files that both load the same module
	buildA := filepath.Join(tmpDir, "a", "BUILD.star")
	buildB := filepath.Join(tmpDir, "b", "BUILD.star")

	err = os.MkdirAll(filepath.Dir(buildA), 0755)
	if err != nil {
		t.Fatal(err)
	}
	err = os.MkdirAll(filepath.Dir(buildB), 0755)
	if err != nil {
		t.Fatal(err)
	}

	err = os.WriteFile(buildA, []byte(`load("//shared.star", "shared_func")
shared_func("target_a")
`), 0644)
	if err != nil {
		t.Fatal(err)
	}

	err = os.WriteFile(buildB, []byte(`load("//shared.star", "shared_func")
shared_func("target_b")
`), 0644)
	if err != nil {
		t.Fatal(err)
	}

	// Load both BUILD files
	loader := StarlarkLoader{}

	pkgA, matched, err := loader.Load(context.Background(), buildA)
	if err != nil {
		t.Fatalf("failed to load package A: %v", err)
	}
	if !matched {
		t.Fatal("package A was not matched")
	}
	if len(pkgA.Targets) != 1 || pkgA.Targets[0].Name != "target_a" {
		t.Fatalf("unexpected targets in package A: %+v", pkgA.Targets)
	}

	pkgB, matched, err := loader.Load(context.Background(), buildB)
	if err != nil {
		t.Fatalf("failed to load package B: %v", err)
	}
	if !matched {
		t.Fatal("package B was not matched")
	}
	if len(pkgB.Targets) != 1 || pkgB.Targets[0].Name != "target_b" {
		t.Fatalf("unexpected targets in package B: %+v", pkgB.Targets)
	}

	// Both packages should have loaded successfully
	// The shared module should have been cached (though we can't directly observe this without instrumentation)
}
