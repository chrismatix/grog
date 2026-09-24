package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestWindowsWorkflow(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("native Windows workflow")
	}
	workspace := filepath.Join(t.TempDir(), "workspace with spaces")
	if err := os.CopyFS(workspace, os.DirFS("integration/test_repos/windows")); err != nil {
		t.Fatal(err)
	}
	cacheRoot := t.TempDir()
	run := func(arguments ...string) string {
		t.Helper()
		command := exec.Command(binaryPath, append([]string{"--color=no"}, arguments...)...)
		command.Dir = workspace
		command.Env = append(os.Environ(), "GROG_ROOT="+cacheRoot)
		output, err := command.CombinedOutput()
		if err != nil {
			t.Fatalf("grog %v: %v\n%s", arguments, err, output)
		}
		return string(output)
	}
	if output := run("list", "//..."); !strings.Contains(output, "//nested/package:build") {
		t.Fatalf("nested label missing: %s", output)
	}
	run("build", "//nested/package:build")
	for _, filePath := range []string{"started.txt", "stopped.txt", "nested/package/result/value.txt"} {
		if _, err := os.Stat(filepath.Join(workspace, filePath)); err != nil {
			t.Fatal(err)
		}
	}
	value, err := os.ReadFile(filepath.Join(workspace, "nested/package/result/value.txt"))
	if err != nil || strings.TrimSpace(string(value)) != "ready" {
		t.Fatalf("resource exports: %q, %v", value, err)
	}
	if output := run("build", "//nested/package:build"); !strings.Contains(output, "cached") {
		t.Fatalf("expected cached build: %s", output)
	}
	if err := os.RemoveAll(filepath.Join(workspace, "nested/package/result")); err != nil {
		t.Fatal(err)
	}
	run("build", "//nested/package:build")
	if _, err := os.Stat(filepath.Join(workspace, "nested/package/result/arguments.txt")); err != nil {
		t.Fatal(err)
	}
	run("taint", "//nested/package:build")
	run("build", "//nested/package:build")
	if output := run("run", "//:app", "--", "two words", "a&b", "/some/path", "//pkg:target", ""); !strings.Contains(output, `arguments: ["two words" "a&b" "/some/path" "//pkg:target" ""]`) {
		t.Fatalf("native binary arguments: %s", output)
	}
	if output := run("run", "//:hello.grog.sh", "--", "two words"); !strings.Contains(output, "script: <two words>") {
		t.Fatalf("script arguments: %s", output)
	}
	run("logs", "//nested/package:build")
	run("traces", "list")
	command := exec.Command(binaryPath, "build", "//:timeout")
	command.Dir = workspace
	command.Env = append(os.Environ(), "GROG_ROOT="+cacheRoot)
	output, err := command.CombinedOutput()
	if err == nil || !strings.Contains(string(output), "timeout after 1s") {
		t.Fatalf("timeout: %v\n%s", err, output)
	}
	run("clean")
}
