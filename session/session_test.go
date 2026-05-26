package session

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"testing"
)

// writeWorkspace creates a minimal grog workspace in a temp dir with a single
// package "pkg" containing a file-output target, and returns the root.
func writeWorkspace(t *testing.T) string {
	t.Helper()
	root := t.TempDir()

	// Isolate the grog cache root so builds are hermetic across test runs.
	cacheRoot := t.TempDir()
	mustWrite(t, filepath.Join(root, "grog.toml"), "root = "+strconv.Quote(cacheRoot)+"\n")
	mustWrite(t, filepath.Join(root, "pkg", "src", "input.txt"), "hello\n")
	mustWrite(t, filepath.Join(root, "pkg", "BUILD.json"), `{
  "targets": [
    {
      "name": "gen",
      "inputs": ["src/*.txt"],
      "command": "cat src/input.txt > output.txt",
      "outputs": ["output.txt"]
    }
  ]
}`)
	return root
}

func keysOf[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

func mustWrite(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func TestSessionBuildFileTarget(t *testing.T) {
	root := writeWorkspace(t)
	ctx := context.Background()

	sess, err := New(ctx, Options{WorkspaceRoot: root})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer sess.Close()

	res, err := sess.Build(ctx, "//pkg:gen")
	if err != nil {
		t.Fatalf("Build: %v", err)
	}

	if res.Label != "//pkg:gen" {
		t.Errorf("Label = %q, want //pkg:gen", res.Label)
	}
	if res.ChangeHash == "" {
		t.Error("ChangeHash is empty")
	}
	if res.OutputHash == "" {
		t.Error("OutputHash is empty")
	}
	if res.CacheHit {
		t.Error("first build should not be a cache hit")
	}

	// The command should have produced the output file.
	if _, err := os.Stat(filepath.Join(root, "pkg", "output.txt")); err != nil {
		t.Errorf("expected output.txt: %v", err)
	}

	// The file output should be exposed on the BuildResult, keyed by its
	// package-relative path, with an absolute Path resolved from workspace_root.
	fileOut, ok := res.Files["output.txt"]
	if !ok {
		t.Fatalf("Files map missing output.txt; got keys %v", keysOf(res.Files))
	}
	if fileOut.Path != filepath.Join(root, "pkg", "output.txt") {
		t.Errorf("Files[output.txt].Path = %q, want workspace-absolute path", fileOut.Path)
	}
	if fileOut.Digest == "" {
		t.Error("Files[output.txt].Digest is empty")
	}

	// Second build of the same label returns the memoized result instantly.
	res2, err := sess.Build(ctx, "//pkg:gen")
	if err != nil {
		t.Fatalf("second Build: %v", err)
	}
	if res2 != res {
		t.Error("expected memoized result on repeat build")
	}
}

func TestSessionBuildAlias(t *testing.T) {
	root := t.TempDir()
	cacheRoot := t.TempDir()
	mustWrite(t, filepath.Join(root, "grog.toml"), "root = "+strconv.Quote(cacheRoot)+"\n")
	mustWrite(t, filepath.Join(root, "pkg", "src", "input.txt"), "hello\n")
	// A target plus an alias pointing at it.
	mustWrite(t, filepath.Join(root, "pkg", "BUILD.json"), `{
  "targets": [
    {
      "name": "gen",
      "inputs": ["src/*.txt"],
      "command": "cat src/input.txt > output.txt",
      "outputs": ["output.txt"]
    }
  ],
  "aliases": [
    { "name": "gen_alias", "actual": "//pkg:gen" }
  ]
}`)

	ctx := context.Background()
	sess, err := New(ctx, Options{WorkspaceRoot: root})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer sess.Close()

	// Building the alias label must return the underlying target's result.
	res, err := sess.Build(ctx, "//pkg:gen_alias")
	if err != nil {
		t.Fatalf("Build(alias): %v", err)
	}
	if res.ChangeHash == "" || res.OutputHash == "" {
		t.Errorf("alias build returned empty hashes: %+v", res)
	}
}

func TestSessionBuildUnknownTarget(t *testing.T) {
	root := writeWorkspace(t)
	ctx := context.Background()

	sess, err := New(ctx, Options{WorkspaceRoot: root})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer sess.Close()

	if _, err := sess.Build(ctx, "//pkg:nope"); err == nil {
		t.Fatal("expected error for unknown target, got nil")
	}
}
