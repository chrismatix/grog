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

	// Second build of the same label returns the memoized result instantly.
	res2, err := sess.Build(ctx, "//pkg:gen")
	if err != nil {
		t.Fatalf("second Build: %v", err)
	}
	if res2 != res {
		t.Error("expected memoized result on repeat build")
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
