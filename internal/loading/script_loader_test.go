package loading

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"grog/internal/config"

	"go.uber.org/zap/zaptest"
)

func writeTempScript(t *testing.T, dir, name, contents string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("failed to create directories for %s: %v", path, err)
	}
	if err := os.WriteFile(path, []byte(contents), 0755); err != nil {
		t.Fatalf("failed to write script: %v", err)
	}
	return path
}

func TestScriptLoaderLoad(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	script := writeTempScript(t, dir, "format.grog.sh", `#!/usr/bin/env bash
# @grog
# name: format
# dependencies:
#   - //tools:prepare
# inputs:
#   - src/**/*.js
# platforms:
#   - linux/arm64
set -e
`)

	loader := ScriptLoader{}
	pkg, matched, err := loader.Load(context.Background(), script)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if !matched {
		t.Fatalf("expected metadata to be detected")
	}
	if len(pkg.Targets) != 1 {
		t.Fatalf("expected one target, got %d", len(pkg.Targets))
	}
	target := pkg.Targets[0]
	if target.Name != "format" {
		t.Fatalf("expected target name to be format, got %s", target.Name)
	}
	if target.BinOutput != "format.grog.sh" {
		t.Fatalf("expected bin output to default to script name, got %s", target.BinOutput)
	}
	if got := target.Dependencies; len(got) != 1 || got[0] != "//tools:prepare" {
		t.Fatalf("unexpected dependencies: %#v", got)
	}
	if len(target.Inputs) == 0 || target.Inputs[0] != "format.grog.sh" {
		t.Fatalf("expected script path to be the first input, got %#v", target.Inputs)
	}
	if target.Platforms == nil || len(target.Platforms) == 0 {
		t.Fatalf("expected platform to be set")
	}
	if target.Platforms[0] != "linux/arm64" {
		t.Fatalf("expected platform linux/arm64, got %v", target.Platforms)
	}
}

func TestScriptLoaderDifferentFormsAnnotation(t *testing.T) {
	t.Parallel()

	scripts := []string{
		// Annotation is optional
		"#!/usr/bin/env bash\necho hi\n",
		// shebang line is optional
		"# @grog\necho \"hi\"",
	}

	for _, scriptText := range scripts {
		dir := t.TempDir()
		script := writeTempScript(t, dir, "no_meta.grog.sh", scriptText)

		loader := ScriptLoader{}
		_, matched, err := loader.Load(t.Context(), script)
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		if !matched {
			t.Fatalf("expected the script loader to be able to load the file")
		}
	}
}

func TestLoadScriptTarget(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	script := writeTempScript(t, dir, filepath.Join("tools", "release.grog.sh"), `#!/usr/bin/env bash
# @grog
# dependencies:
#   - //build:tool
# tags:
#   - no-cache
echo ok
`)

	originalRoot := config.Global.WorkspaceRoot
	config.Global.WorkspaceRoot = dir
	t.Cleanup(func() {
		config.Global.WorkspaceRoot = originalRoot
	})

	logger := zaptest.NewLogger(t).Sugar()
	target, err := LoadScriptTarget(context.Background(), logger, script)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if target.Label.Package != "tools" {
		t.Fatalf("expected package tools, got %s", target.Label.Package)
	}
	if target.Label.Name != "release.grog.sh" {
		t.Fatalf("expected default target name to include extension, got %s", target.Label.Name)
	}
	if !target.HasBinOutput() || target.BinOutput.Identifier != "release.grog.sh" {
		t.Fatalf("unexpected bin output: %#v", target.BinOutput)
	}
	if len(target.Dependencies) != 1 || target.Dependencies[0].String() != "//build:tool" {
		t.Fatalf("unexpected dependencies: %#v", target.Dependencies)
	}
	if len(target.Tags) != 1 || target.Tags[0] != "no-cache" {
		t.Fatalf("unexpected tags: %#v", target.Tags)
	}
}
