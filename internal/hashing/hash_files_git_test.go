package hashing

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// initGitRepo creates a throwaway git repository rooted at dir and returns a
// helper to run git commands within it.
func initGitRepo(t *testing.T, dir string) func(args ...string) {
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

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestHashFilesAtRef_MatchesWorkingTreeForSameContent(t *testing.T) {
	dir := t.TempDir()
	git := initGitRepo(t, dir)

	pkgDir := filepath.Join(dir, "services", "api")
	writeFile(t, filepath.Join(pkgDir, "a.txt"), "alpha\n")
	writeFile(t, filepath.Join(pkgDir, "b.txt"), "bravo\n")
	git("add", "-A")
	git("commit", "-q", "-m", "initial")

	inputs := []string{"a.txt", "b.txt"}

	// The working-tree hash and the at-ref (HEAD) hash must agree because the
	// committed content equals the on-disk content.
	workingTreeHash, err := HashFiles(pkgDir, inputs)
	if err != nil {
		t.Fatalf("HashFiles: %v", err)
	}
	atRefHash, err := HashFilesAtRef(dir, "HEAD", pkgDir, inputs)
	if err != nil {
		t.Fatalf("HashFilesAtRef: %v", err)
	}
	if workingTreeHash != atRefHash {
		t.Fatalf("at-ref hash should match working-tree hash for identical content:\n  working: %q\n  at-ref:  %q",
			workingTreeHash, atRefHash)
	}
}

func TestHashFilesAtRef_ReflectsPriorContent(t *testing.T) {
	dir := t.TempDir()
	git := initGitRepo(t, dir)

	pkgDir := filepath.Join(dir, "pkg")
	lockfile := filepath.Join(pkgDir, "lock.json")

	writeFile(t, lockfile, "v1\n")
	git("add", "-A")
	git("commit", "-q", "-m", "v1")

	inputs := []string{"lock.json"}
	priorHash, err := HashFilesAtRef(dir, "HEAD", pkgDir, inputs)
	if err != nil {
		t.Fatalf("HashFilesAtRef (prior): %v", err)
	}

	// Change the lockfile and commit again.
	writeFile(t, lockfile, "v2-with-more-deps\n")
	git("add", "-A")
	git("commit", "-q", "-m", "v2")

	currentHash, err := HashFiles(pkgDir, inputs)
	if err != nil {
		t.Fatalf("HashFiles (current): %v", err)
	}
	priorAtHeadParent, err := HashFilesAtRef(dir, "HEAD~1", pkgDir, inputs)
	if err != nil {
		t.Fatalf("HashFilesAtRef (HEAD~1): %v", err)
	}

	// The prior commit's hash (captured before the change, and recomputed via
	// HEAD~1) must equal each other and differ from the current content hash.
	if priorHash != priorAtHeadParent {
		t.Fatalf("prior hash mismatch:\n  captured: %q\n  HEAD~1:   %q", priorHash, priorAtHeadParent)
	}
	if priorHash == currentHash {
		t.Fatalf("prior and current hashes must differ after a content change, both %q", priorHash)
	}
}

func TestHashFilesAtRef_SkipsFilesAbsentAtRef(t *testing.T) {
	dir := t.TempDir()
	git := initGitRepo(t, dir)

	pkgDir := filepath.Join(dir, "pkg")
	writeFile(t, filepath.Join(pkgDir, "present.txt"), "here\n")
	git("add", "-A")
	git("commit", "-q", "-m", "only present.txt")

	// "missing.txt" does not exist at HEAD; HashFilesAtRef must skip it exactly
	// as HashFiles skips a missing working-tree file, so the two agree.
	inputs := []string{"present.txt", "missing.txt"}

	atRef, err := HashFilesAtRef(dir, "HEAD", pkgDir, inputs)
	if err != nil {
		t.Fatalf("HashFilesAtRef: %v", err)
	}
	// Only present.txt exists on disk too (missing.txt was never written).
	working, err := HashFiles(pkgDir, inputs)
	if err != nil {
		t.Fatalf("HashFiles: %v", err)
	}
	if atRef != working {
		t.Fatalf("skipping an absent file should match HashFiles:\n  at-ref:  %q\n  working: %q", atRef, working)
	}
}
