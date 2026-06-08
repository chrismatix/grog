package config

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func initGitRepoForRef(t *testing.T, dir string) func(args ...string) {
	t.Helper()
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=test",
			"GIT_AUTHOR_EMAIL=test@example.com",
			"GIT_COMMITTER_NAME=test",
			"GIT_COMMITTER_EMAIL=test@example.com",
		)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v failed: %v\n%s", args, err, out)
		}
	}
	run("init", "-q")
	return run
}

// chdir switches to dir for the duration of the test.
func chdir(t *testing.T, dir string) {
	t.Helper()
	orig, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(orig) })
}

func TestResolveCacheSeedRef_ExplicitWins(t *testing.T) {
	// An explicit ref is returned verbatim without consulting git.
	ref, ok := ResolveCacheSeedRef("origin/release-1.2")
	if !ok || ref != "origin/release-1.2" {
		t.Fatalf("explicit ref: got (%q, %v), want (\"origin/release-1.2\", true)", ref, ok)
	}
}

func TestResolveCacheSeedRef_AutoDetectFallsBackToHeadParent(t *testing.T) {
	dir := t.TempDir()
	git := initGitRepoForRef(t, dir)

	// Two commits, no remote: origin/HEAD is absent, so resolution falls back
	// to HEAD~1.
	if err := os.WriteFile(filepath.Join(dir, "f.txt"), []byte("one\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	git("add", "-A")
	git("commit", "-q", "-m", "one")
	if err := os.WriteFile(filepath.Join(dir, "f.txt"), []byte("two\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	git("add", "-A")
	git("commit", "-q", "-m", "two")

	chdir(t, dir)
	ref, ok := ResolveCacheSeedRef("")
	if !ok || ref != "HEAD~1" {
		t.Fatalf("auto-detect: got (%q, %v), want (\"HEAD~1\", true)", ref, ok)
	}
}

func TestResolveCacheSeedRef_NoParentReturnsFalse(t *testing.T) {
	dir := t.TempDir()
	git := initGitRepoForRef(t, dir)

	// A single commit has no parent and no remote default branch: resolution
	// cannot find a usable seed ref.
	if err := os.WriteFile(filepath.Join(dir, "f.txt"), []byte("only\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	git("add", "-A")
	git("commit", "-q", "-m", "only")

	chdir(t, dir)
	if ref, ok := ResolveCacheSeedRef(""); ok {
		t.Fatalf("single-commit repo should not resolve a seed ref, got (%q, %v)", ref, ok)
	}
}

func TestGetGitRoot(t *testing.T) {
	dir := t.TempDir()
	initGitRepoForRef(t, dir)
	chdir(t, dir)

	root, err := GetGitRoot()
	if err != nil {
		t.Fatalf("GetGitRoot: %v", err)
	}
	// Resolve symlinks because TempDir on macOS is under /var -> /private/var.
	wantRoot, _ := filepath.EvalSymlinks(dir)
	gotRoot, _ := filepath.EvalSymlinks(root)
	if gotRoot != wantRoot {
		t.Fatalf("GetGitRoot() = %q, want %q", gotRoot, wantRoot)
	}
}
