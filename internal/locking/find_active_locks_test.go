package locking

import (
	"os"
	"path/filepath"
	"strconv"
	"testing"
)

// writeLockFile creates "$grogRoot/<prefix>/lockfile" with the given contents.
func writeLockFile(t *testing.T, grogRoot, prefix, contents string) string {
	t.Helper()
	dir := filepath.Join(grogRoot, prefix)
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatalf("failed to create lock dir: %v", err)
	}
	lockFilePath := filepath.Join(dir, "lockfile")
	if err := os.WriteFile(lockFilePath, []byte(contents), 0644); err != nil {
		t.Fatalf("failed to write lock file: %v", err)
	}
	return lockFilePath
}

// deadPID returns a process id that is (almost) certainly not running. The
// existing stale-lock test uses 999999; we reuse the same convention.
const deadPID = 999999

func TestFindActiveLocks_DetectsLiveLock(t *testing.T) {
	grogRoot := t.TempDir()
	livePID := os.Getpid()
	lockFilePath := writeLockFile(
		t,
		grogRoot,
		"abcdef0123456789-myrepo",
		buildLockFileContents(livePID, []string{"grog", "build", "//foo:bar"}),
	)

	locks, err := FindActiveLocks(grogRoot)
	if err != nil {
		t.Fatalf("FindActiveLocks returned error: %v", err)
	}
	if len(locks) != 1 {
		t.Fatalf("expected 1 active lock, got %d: %+v", len(locks), locks)
	}
	got := locks[0]
	if got.ProcessID != livePID {
		t.Errorf("ProcessID = %d, want %d", got.ProcessID, livePID)
	}
	if got.Command != "grog build //foo:bar" {
		t.Errorf("Command = %q, want %q", got.Command, "grog build //foo:bar")
	}
	if got.LockFilePath != lockFilePath {
		t.Errorf("LockFilePath = %q, want %q", got.LockFilePath, lockFilePath)
	}
}

func TestFindActiveLocks_SkipsStaleLock(t *testing.T) {
	grogRoot := t.TempDir()
	writeLockFile(t, grogRoot, "deadbeef0badf00d-stale", strconv.Itoa(deadPID))

	locks, err := FindActiveLocks(grogRoot)
	if err != nil {
		t.Fatalf("FindActiveLocks returned error: %v", err)
	}
	if len(locks) != 0 {
		t.Fatalf("expected stale lock to be skipped, got %d: %+v", len(locks), locks)
	}
}

func TestFindActiveLocks_IgnoresNonWorkspaceDirsAndFiles(t *testing.T) {
	grogRoot := t.TempDir()

	// Shared cache directories have no lockfile and must be ignored.
	for _, name := range []string{"cas", "cache", "traces"} {
		if err := os.MkdirAll(filepath.Join(grogRoot, name), 0755); err != nil {
			t.Fatalf("failed to create %s dir: %v", name, err)
		}
	}
	// A stray regular file at the root must not be treated as a workspace dir.
	if err := os.WriteFile(filepath.Join(grogRoot, "lockfile"), []byte("123"), 0644); err != nil {
		t.Fatalf("failed to write stray file: %v", err)
	}

	locks, err := FindActiveLocks(grogRoot)
	if err != nil {
		t.Fatalf("FindActiveLocks returned error: %v", err)
	}
	if len(locks) != 0 {
		t.Fatalf("expected no active locks, got %d: %+v", len(locks), locks)
	}
}

func TestFindActiveLocks_MissingRootReturnsNoError(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "does-not-exist")

	locks, err := FindActiveLocks(missing)
	if err != nil {
		t.Fatalf("expected nil error for missing root, got: %v", err)
	}
	if locks != nil {
		t.Fatalf("expected nil locks for missing root, got: %+v", locks)
	}
}

func TestFindActiveLocks_SkipsUnparseableLockFile(t *testing.T) {
	grogRoot := t.TempDir()
	writeLockFile(t, grogRoot, "0123456789abcdef-garbage", "not-a-pid")

	locks, err := FindActiveLocks(grogRoot)
	if err != nil {
		t.Fatalf("FindActiveLocks returned error: %v", err)
	}
	if len(locks) != 0 {
		t.Fatalf("expected unparseable lock to be skipped, got %d: %+v", len(locks), locks)
	}
}

func TestFindActiveLocks_SortedByPath(t *testing.T) {
	grogRoot := t.TempDir()
	livePID := os.Getpid()
	// Create out of lexical order to verify sorting.
	writeLockFile(t, grogRoot, "cccccccccccccccc-zebra", strconv.Itoa(livePID))
	writeLockFile(t, grogRoot, "aaaaaaaaaaaaaaaa-alpha", strconv.Itoa(livePID))
	writeLockFile(t, grogRoot, "bbbbbbbbbbbbbbbb-bravo", strconv.Itoa(livePID))

	locks, err := FindActiveLocks(grogRoot)
	if err != nil {
		t.Fatalf("FindActiveLocks returned error: %v", err)
	}
	if len(locks) != 3 {
		t.Fatalf("expected 3 active locks, got %d: %+v", len(locks), locks)
	}
	for index := 1; index < len(locks); index++ {
		if locks[index-1].LockFilePath > locks[index].LockFilePath {
			t.Fatalf(
				"locks not sorted by path: %q came before %q",
				locks[index-1].LockFilePath,
				locks[index].LockFilePath,
			)
		}
	}
}
