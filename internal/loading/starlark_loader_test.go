package loading

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
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

func TestStarlarkLoader_GrogEnvFile(t *testing.T) {
	t.Run("set to resolved absolute path when env file is configured", func(t *testing.T) {
		// Bug caught: GROG_ENV_FILE not injected as a predeclared variable,
		// leaving Starlark scripts unable to reference the environment
		// variables file path.
		tmpDir := t.TempDir()

		oldConfig := config.Global
		defer func() { config.Global = oldConfig }()

		config.Global.WorkspaceRoot = tmpDir
		config.Global.EnvironmentVariablesFile = "env.vars"

		// The env file doesn't need to contain anything for the path test,
		// but create it so the workspace is realistic.
		envFilePath := filepath.Join(tmpDir, "env.vars")
		if err := os.WriteFile(envFilePath, []byte("FOO=bar\n"), 0644); err != nil {
			t.Fatal(err)
		}

		// BUILD.star captures GROG_ENV_FILE into a target command.
		buildFile := filepath.Join(tmpDir, "BUILD.star")
		script := `target(name = "test", command = GROG_ENV_FILE)`
		if err := os.WriteFile(buildFile, []byte(script), 0644); err != nil {
			t.Fatal(err)
		}

		loader := StarlarkLoader{}
		pkg, matched, err := loader.Load(context.Background(), buildFile)
		if err != nil {
			t.Fatalf("Load() returned error: %v", err)
		}
		if !matched {
			t.Fatal("expected loader to match BUILD.star")
		}

		expectedPath := filepath.Join(tmpDir, "env.vars")
		if len(pkg.Targets) != 1 {
			t.Fatalf("expected 1 target, got %d", len(pkg.Targets))
		}
		if pkg.Targets[0].Command != expectedPath {
			t.Errorf("GROG_ENV_FILE = %q, want %q", pkg.Targets[0].Command, expectedPath)
		}
	})

	t.Run("absolute path passed through unchanged", func(t *testing.T) {
		// Bug caught: absolute EnvironmentVariablesFile paths double-joined
		// with WorkspaceRoot, producing an incorrect GROG_ENV_FILE value.
		tmpDir := t.TempDir()

		oldConfig := config.Global
		defer func() { config.Global = oldConfig }()

		absoluteEnvPath := filepath.Join(tmpDir, "absolute", "env.vars")
		config.Global.WorkspaceRoot = tmpDir
		config.Global.EnvironmentVariablesFile = absoluteEnvPath

		buildFile := filepath.Join(tmpDir, "BUILD.star")
		script := `target(name = "test", command = GROG_ENV_FILE)`
		if err := os.WriteFile(buildFile, []byte(script), 0644); err != nil {
			t.Fatal(err)
		}

		loader := StarlarkLoader{}
		pkg, _, err := loader.Load(context.Background(), buildFile)
		if err != nil {
			t.Fatalf("Load() returned error: %v", err)
		}

		if len(pkg.Targets) != 1 {
			t.Fatalf("expected 1 target, got %d", len(pkg.Targets))
		}
		if pkg.Targets[0].Command != absoluteEnvPath {
			t.Errorf("GROG_ENV_FILE = %q, want %q", pkg.Targets[0].Command, absoluteEnvPath)
		}
	})

	t.Run("empty string when env file is not configured", func(t *testing.T) {
		// Bug caught: GROG_ENV_FILE undefined (Starlark error) instead of
		// being set to "" when no environment_variables_file is configured.
		tmpDir := t.TempDir()

		oldConfig := config.Global
		defer func() { config.Global = oldConfig }()

		config.Global.WorkspaceRoot = tmpDir
		config.Global.EnvironmentVariablesFile = ""

		buildFile := filepath.Join(tmpDir, "BUILD.star")
		// Use string concatenation so we can verify the value is truly empty.
		script := `target(name = "test", command = "prefix:" + GROG_ENV_FILE + ":suffix")`
		if err := os.WriteFile(buildFile, []byte(script), 0644); err != nil {
			t.Fatal(err)
		}

		loader := StarlarkLoader{}
		pkg, _, err := loader.Load(context.Background(), buildFile)
		if err != nil {
			t.Fatalf("Load() returned error: %v", err)
		}

		if len(pkg.Targets) != 1 {
			t.Fatalf("expected 1 target, got %d", len(pkg.Targets))
		}
		if pkg.Targets[0].Command != "prefix::suffix" {
			t.Errorf("GROG_ENV_FILE should be empty, got command %q, want %q",
				pkg.Targets[0].Command, "prefix::suffix")
		}
	})

	t.Run("existing predeclared variables remain unchanged", func(t *testing.T) {
		// Bug caught: adding GROG_ENV_FILE accidentally overwrites or removes
		// existing predeclared variables like GROG_OS or GROG_ARCH.
		tmpDir := t.TempDir()

		oldConfig := config.Global
		defer func() { config.Global = oldConfig }()

		config.Global.WorkspaceRoot = tmpDir
		config.Global.OS = "linux"
		config.Global.Arch = "amd64"
		config.Global.EnvironmentVariablesFile = "env.vars"

		envFilePath := filepath.Join(tmpDir, "env.vars")
		if err := os.WriteFile(envFilePath, []byte("FOO=bar\n"), 0644); err != nil {
			t.Fatal(err)
		}

		buildFile := filepath.Join(tmpDir, "BUILD.star")
		script := `target(name = "env_file", command = GROG_ENV_FILE)
target(name = "os", command = GROG_OS)
target(name = "arch", command = GROG_ARCH)
`
		if err := os.WriteFile(buildFile, []byte(script), 0644); err != nil {
			t.Fatal(err)
		}

		loader := StarlarkLoader{}
		pkg, _, err := loader.Load(context.Background(), buildFile)
		if err != nil {
			t.Fatalf("Load() returned error: %v", err)
		}

		targetsByName := make(map[string]*TargetDTO)
		for _, target := range pkg.Targets {
			targetsByName[target.Name] = target
		}

		if targetsByName["os"].Command != "linux" {
			t.Errorf("GROG_OS = %q, want %q", targetsByName["os"].Command, "linux")
		}
		if targetsByName["arch"].Command != "amd64" {
			t.Errorf("GROG_ARCH = %q, want %q", targetsByName["arch"].Command, "amd64")
		}
		expectedPath := filepath.Join(tmpDir, "env.vars")
		if targetsByName["env_file"].Command != expectedPath {
			t.Errorf("GROG_ENV_FILE = %q, want %q", targetsByName["env_file"].Command, expectedPath)
		}
	})

	t.Run("available in loaded star modules", func(t *testing.T) {
		// Bug caught: GROG_ENV_FILE injected into the main BUILD.star predeclared
		// dict but not into the loadModule predeclared dict, making it undefined
		// in load()'d modules.
		tmpDir := t.TempDir()

		oldConfig := config.Global
		defer func() { config.Global = oldConfig }()

		config.Global.WorkspaceRoot = tmpDir
		config.Global.EnvironmentVariablesFile = "env.vars"

		envFilePath := filepath.Join(tmpDir, "env.vars")
		if err := os.WriteFile(envFilePath, []byte("FOO=bar\n"), 0644); err != nil {
			t.Fatal(err)
		}

		// Module that accesses GROG_ENV_FILE
		modulePath := filepath.Join(tmpDir, "helpers.star")
		moduleScript := `def create_target():
    target(name = "from_module", command = GROG_ENV_FILE)
`
		if err := os.WriteFile(modulePath, []byte(moduleScript), 0644); err != nil {
			t.Fatal(err)
		}

		buildFile := filepath.Join(tmpDir, "BUILD.star")
		buildScript := `load("//helpers.star", "create_target")
create_target()
`
		if err := os.WriteFile(buildFile, []byte(buildScript), 0644); err != nil {
			t.Fatal(err)
		}

		loader := StarlarkLoader{}
		pkg, _, err := loader.Load(context.Background(), buildFile)
		if err != nil {
			t.Fatalf("Load() returned error: %v", err)
		}

		expectedPath := filepath.Join(tmpDir, "env.vars")
		if len(pkg.Targets) != 1 {
			t.Fatalf("expected 1 target, got %d", len(pkg.Targets))
		}
		if pkg.Targets[0].Command != expectedPath {
			t.Errorf("GROG_ENV_FILE in module = %q, want %q",
				pkg.Targets[0].Command, expectedPath)
		}
	})
}

func TestStarlarkLoader_OciPush(t *testing.T) {
	tmpDir := t.TempDir()
	oldWorkspaceRoot := config.Global.WorkspaceRoot
	config.Global.WorkspaceRoot = tmpDir
	defer func() { config.Global.WorkspaceRoot = oldWorkspaceRoot }()

	build := filepath.Join(tmpDir, "BUILD.star")
	if err := os.WriteFile(build, []byte(`target(
    name = "app",
    command = "docker build -t app .",
    outputs = ["oci::app"],
    oci_push = {
        "app": "registry.org/app:1.0.0",
        "worker": ["registry.org/worker:1.0.0", "registry.org/worker:latest"],
    },
)
`), 0644); err != nil {
		t.Fatal(err)
	}

	pkg, _, err := (StarlarkLoader{}).Load(context.Background(), build)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(pkg.Targets) != 1 {
		t.Fatalf("expected 1 target, got %d", len(pkg.Targets))
	}
	push := pkg.Targets[0].OciPush
	if got, want := push["app"], (ociPushDestinations{"registry.org/app:1.0.0"}); !reflect.DeepEqual(got, want) {
		t.Errorf("scalar destination not normalised: %v", got)
	}
	if got, want := push["worker"], (ociPushDestinations{"registry.org/worker:1.0.0", "registry.org/worker:latest"}); !reflect.DeepEqual(got, want) {
		t.Errorf("list destinations preserved wrong: %v, want %v", got, want)
	}
}

func TestStarlarkLoader_StdlibModules(t *testing.T) {
	tmpDir := t.TempDir()

	oldWorkspaceRoot := config.Global.WorkspaceRoot
	config.Global.WorkspaceRoot = tmpDir
	defer func() {
		config.Global.WorkspaceRoot = oldWorkspaceRoot
	}()

	tests := []struct {
		name    string
		script  string
		wantErr bool
	}{
		{
			name: "json.encode produces valid JSON",
			script: `result = json.encode({"key": "value", "list": [1, 2, 3]})
target(name = "test", command = "echo " + result)
`,
		},
		{
			name: "json.decode round-trips with encode",
			script: `encoded = json.encode({"hello": "world"})
decoded = json.decode(encoded)
target(name = "test", command = "echo " + decoded["hello"])
`,
		},
		{
			name: "json.encode handles special characters",
			script: `result = json.encode({"dep": "foo >= 1.0, <2.0", "quotes": "say \"hello\""})
target(name = "test", command = "echo " + result)
`,
		},
		{
			name: "math module is available",
			script: `x = math.floor(3.7)
target(name = "test", command = "echo " + str(x))
`,
		},
		{
			name: "time module is available",
			script: `d = time.parse_duration("1h30m")
target(name = "test", command = "echo " + str(d))
`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			buildFile := filepath.Join(tmpDir, tt.name, "BUILD.star")
			if err := os.MkdirAll(filepath.Dir(buildFile), 0755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(buildFile, []byte(tt.script), 0644); err != nil {
				t.Fatal(err)
			}

			loader := StarlarkLoader{}
			_, _, err := loader.Load(context.Background(), buildFile)
			if (err != nil) != tt.wantErr {
				t.Fatalf("Load() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}
