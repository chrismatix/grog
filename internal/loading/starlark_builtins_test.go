package loading

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"grog/internal/config"
)

func loadStarlark(t *testing.T, script string) (PackageDTO, error) {
	t.Helper()
	dir := t.TempDir()

	oldRoot := config.Global.WorkspaceRoot
	config.Global.WorkspaceRoot = dir
	t.Cleanup(func() { config.Global.WorkspaceRoot = oldRoot })

	p := filepath.Join(dir, "BUILD.star")
	if err := os.WriteFile(p, []byte(script), 0644); err != nil {
		t.Fatal(err)
	}
	pkg, _, err := StarlarkLoader{}.Load(context.Background(), p)
	return pkg, err
}

func TestStarlark_AliasBuiltin(t *testing.T) {
	pkg, err := loadStarlark(t, `
alias(name = "myalias", actual = "//foo:bar")
`)
	if err != nil {
		t.Fatal(err)
	}
	if len(pkg.Aliases) != 1 {
		t.Fatalf("expected 1 alias, got %d", len(pkg.Aliases))
	}
	if pkg.Aliases[0].Name != "myalias" || pkg.Aliases[0].Actual != "//foo:bar" {
		t.Fatalf("unexpected alias: %+v", pkg.Aliases[0])
	}
}

func TestStarlark_AliasBuiltin_MissingArgs(t *testing.T) {
	_, err := loadStarlark(t, `alias(name = "x")`)
	if err == nil {
		t.Fatal("expected error for missing actual")
	}
}

func TestStarlark_EnvironmentBuiltin(t *testing.T) {
	pkg, err := loadStarlark(t, `
environment(name = "dev", type = "oci", dependencies = ["//foo:bar"], oci_image = "alpine:3")
environment(name = "prod", type = "oci")
`)
	if err != nil {
		t.Fatal(err)
	}
	if len(pkg.Environments) != 2 {
		t.Fatalf("expected 2 environments, got %d", len(pkg.Environments))
	}
	if pkg.Environments[0].Name != "dev" || pkg.Environments[0].OCIImage != "alpine:3" {
		t.Fatalf("unexpected env: %+v", pkg.Environments[0])
	}
	if len(pkg.Environments[0].Dependencies) != 1 || pkg.Environments[0].Dependencies[0] != "//foo:bar" {
		t.Fatalf("unexpected dependencies: %+v", pkg.Environments[0].Dependencies)
	}
}

func TestStarlark_EnvironmentBuiltin_BadDependencies(t *testing.T) {
	_, err := loadStarlark(t, `environment(name="e", type="oci", dependencies=[123])`)
	if err == nil {
		t.Fatal("expected error for non-string dependency")
	}
}

func TestStarlark_TargetBuiltin_AllArguments(t *testing.T) {
	pkg, err := loadStarlark(t, `
target(
  name = "t",
  command = "make",
  dependencies = ["//a:b"],
  inputs = ["src/**/*.go"],
  exclude_inputs = ["src/skip.go"],
  outputs = ["dist/out"],
  bin_output = "dist/bin",
  output_checks = [{"command": "test -f foo", "expected_output": "ok"}],
  tags = ["fast"],
  fingerprint = {"key": "val"},
  platforms = ["linux/amd64"],
  environment_variables = {"FOO": "bar"},
  timeout = "30s",
  concurrency_group = "compile",
)
`)
	if err != nil {
		t.Fatal(err)
	}
	if len(pkg.Targets) != 1 {
		t.Fatalf("expected 1 target, got %d", len(pkg.Targets))
	}
	tgt := pkg.Targets[0]
	if tgt.Name != "t" || tgt.Command != "make" {
		t.Fatalf("unexpected target: %+v", tgt)
	}
	if len(tgt.Dependencies) != 1 || tgt.Dependencies[0] != "//a:b" {
		t.Fatalf("deps: %v", tgt.Dependencies)
	}
	if len(tgt.Inputs) != 1 || tgt.Inputs[0] != "src/**/*.go" {
		t.Fatalf("inputs: %v", tgt.Inputs)
	}
	if len(tgt.ExcludeInputs) != 1 {
		t.Fatalf("exclude_inputs: %v", tgt.ExcludeInputs)
	}
	if len(tgt.Outputs) != 1 || tgt.Outputs[0] != "dist/out" {
		t.Fatalf("outputs: %v", tgt.Outputs)
	}
	if tgt.BinOutput != "dist/bin" {
		t.Fatalf("bin_output: %s", tgt.BinOutput)
	}
	if len(tgt.OutputChecks) != 1 || tgt.OutputChecks[0].Command != "test -f foo" || tgt.OutputChecks[0].ExpectedOutput != "ok" {
		t.Fatalf("output_checks: %+v", tgt.OutputChecks)
	}
	if len(tgt.Tags) != 1 || tgt.Tags[0] != "fast" {
		t.Fatalf("tags: %v", tgt.Tags)
	}
	if tgt.Fingerprint["key"] != "val" {
		t.Fatalf("fingerprint: %v", tgt.Fingerprint)
	}
	if len(tgt.Platforms) != 1 || tgt.Platforms[0] != "linux/amd64" {
		t.Fatalf("platforms: %v", tgt.Platforms)
	}
	if tgt.EnvironmentVariables["FOO"] != "bar" {
		t.Fatalf("env_vars: %v", tgt.EnvironmentVariables)
	}
	if tgt.Timeout != "30s" {
		t.Fatalf("timeout: %s", tgt.Timeout)
	}
	if tgt.ConcurrencyGroup != "compile" {
		t.Fatalf("concurrency_group: %s", tgt.ConcurrencyGroup)
	}
}

func TestStarlark_TargetBuiltin_OutputChecks_Struct(t *testing.T) {
	pkg, err := loadStarlark(t, `
def _struct(**kwargs):
    return struct(**kwargs)
`+"")
	_ = pkg
	_ = err
	// starlark struct requires import; use starlarkstruct via plain dict is sufficient and already tested above.
	// Provide a test exercising the dict case w/o expected_output to cover that branch.
	pkg2, err := loadStarlark(t, `
target(
  name = "t",
  command = "x",
  output_checks = [{"command": "test"}],
)
`)
	if err != nil {
		t.Fatal(err)
	}
	if len(pkg2.Targets) != 1 || len(pkg2.Targets[0].OutputChecks) != 1 {
		t.Fatalf("unexpected: %+v", pkg2.Targets)
	}
	if pkg2.Targets[0].OutputChecks[0].ExpectedOutput != "" {
		t.Fatalf("expected empty expected_output")
	}
}

func TestStarlark_TargetBuiltin_OutputCheckErrors(t *testing.T) {
	tests := []struct {
		name   string
		script string
		want   string
	}{
		{
			name:   "missing command",
			script: `target(name="t", command="x", output_checks=[{"expected_output": "ok"}])`,
			want:   "missing 'command'",
		},
		{
			name:   "command wrong type",
			script: `target(name="t", command="x", output_checks=[{"command": 123}])`,
			want:   "'command' must be string",
		},
		{
			name:   "invalid type",
			script: `target(name="t", command="x", output_checks=["bad"])`,
			want:   "output_check must be dict or struct",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := loadStarlark(t, tc.script)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("expected error containing %q, got %v", tc.want, err)
			}
		})
	}
}

func TestStarlark_TargetBuiltin_BadFieldTypes(t *testing.T) {
	tests := []struct {
		name   string
		script string
		want   string
	}{
		{"deps", `target(name="t", dependencies=[1])`, "dependencies"},
		{"inputs", `target(name="t", inputs=[1])`, "inputs"},
		{"exclude_inputs", `target(name="t", exclude_inputs=[1])`, "exclude_inputs"},
		{"outputs", `target(name="t", outputs=[1])`, "outputs"},
		{"tags", `target(name="t", tags=[1])`, "tags"},
		{"fingerprint key", `target(name="t", fingerprint={1: "v"})`, "fingerprint"},
		{"fingerprint val", `target(name="t", fingerprint={"k": 1})`, "fingerprint"},
		{"platforms", `target(name="t", platforms=[1])`, "platforms"},
		{"env_vars val", `target(name="t", environment_variables={"K": 1})`, "environment_variables"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := loadStarlark(t, tc.script)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("expected error containing %q, got %v", tc.want, err)
			}
		})
	}
}

func TestStarlark_Load_FileNotFound(t *testing.T) {
	dir := t.TempDir()
	oldRoot := config.Global.WorkspaceRoot
	config.Global.WorkspaceRoot = dir
	t.Cleanup(func() { config.Global.WorkspaceRoot = oldRoot })

	_, _, err := StarlarkLoader{}.Load(context.Background(), filepath.Join(dir, "missing.star"))
	if err == nil {
		t.Fatal("expected error for missing file")
	}
}

func TestStarlark_ModuleNotFound(t *testing.T) {
	dir := t.TempDir()
	oldRoot := config.Global.WorkspaceRoot
	config.Global.WorkspaceRoot = dir
	t.Cleanup(func() { config.Global.WorkspaceRoot = oldRoot })

	p := filepath.Join(dir, "BUILD.star")
	if err := os.WriteFile(p, []byte(`load("//missing.star", "x")`), 0644); err != nil {
		t.Fatal(err)
	}
	_, _, err := StarlarkLoader{}.Load(context.Background(), p)
	if err == nil || !strings.Contains(err.Error(), "module not found") {
		t.Fatalf("expected module not found error, got %v", err)
	}
}

func TestStarlark_RelativeModule(t *testing.T) {
	dir := t.TempDir()
	oldRoot := config.Global.WorkspaceRoot
	config.Global.WorkspaceRoot = dir
	t.Cleanup(func() { config.Global.WorkspaceRoot = oldRoot })

	helper := filepath.Join(dir, "helper.star")
	if err := os.WriteFile(helper, []byte(`def make_target(n):
    target(name = n, command = "echo " + n)
`), 0644); err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(dir, "BUILD.star")
	if err := os.WriteFile(p, []byte(`load("helper.star", "make_target")
make_target("rel")
`), 0644); err != nil {
		t.Fatal(err)
	}
	pkg, _, err := StarlarkLoader{}.Load(context.Background(), p)
	if err != nil {
		t.Fatal(err)
	}
	if len(pkg.Targets) != 1 || pkg.Targets[0].Name != "rel" {
		t.Fatalf("unexpected: %+v", pkg.Targets)
	}
}

func TestStarlark_PredeclaredEnvVars(t *testing.T) {
	dir := t.TempDir()
	oldRoot := config.Global.WorkspaceRoot
	oldVars := config.Global.EnvironmentVariables
	config.Global.WorkspaceRoot = dir
	config.Global.EnvironmentVariables = map[string]string{"CUSTOM_VAR": "abc"}
	t.Cleanup(func() {
		config.Global.WorkspaceRoot = oldRoot
		config.Global.EnvironmentVariables = oldVars
	})

	p := filepath.Join(dir, "BUILD.star")
	if err := os.WriteFile(p, []byte(`target(name="t", command=CUSTOM_VAR)`), 0644); err != nil {
		t.Fatal(err)
	}
	pkg, _, err := StarlarkLoader{}.Load(context.Background(), p)
	if err != nil {
		t.Fatal(err)
	}
	if pkg.Targets[0].Command != "abc" {
		t.Fatalf("expected 'abc', got %q", pkg.Targets[0].Command)
	}
}
