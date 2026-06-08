package locking

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestBuildLockFileContents(t *testing.T) {
	if got := buildLockFileContents(123, nil); got != "123" {
		t.Fatalf("got %q", got)
	}
	if got := buildLockFileContents(123, []string{"grog", "build", "//pkg"}); got != "123\tgrog build //pkg" {
		t.Fatalf("got %q", got)
	}
}

func TestParseLockFileContents(t *testing.T) {
	pid, cmd, err := parseLockFileContents("123\tgrog build")
	if err != nil || pid != 123 || cmd != "grog build" {
		t.Fatalf("got pid=%d cmd=%q err=%v", pid, cmd, err)
	}
	pid, cmd, err = parseLockFileContents("123")
	if err != nil || pid != 123 || cmd != "" {
		t.Fatal("plain pid")
	}
	if _, _, err := parseLockFileContents(""); err == nil {
		t.Fatal("expected empty err")
	}
	if _, _, err := parseLockFileContents("abc"); err == nil {
		t.Fatal("expected parse err")
	}
}

func TestProcessRunning(t *testing.T) {
	if processRunning(0) {
		t.Fatal("0")
	}
	if processRunning(-1) {
		t.Fatal("negative")
	}
	if !processRunning(os.Getpid()) {
		t.Fatal("self")
	}
}

func TestWorkspaceLocker_LockUnlock(t *testing.T) {
	root := setupTestWorkspace(t)
	wl := NewWorkspaceLocker()
	ctx := context.Background()

	if err := wl.Lock(ctx); err != nil {
		t.Fatalf("Lock: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "logs")); err != nil {
		// just smoke: workspace root exists
	}
	if err := wl.Unlock(); err != nil {
		t.Fatalf("Unlock: %v", err)
	}
}

func TestWorkspaceLocker_StaleLockReplaced(t *testing.T) {
	setupTestWorkspace(t)
	wl := NewWorkspaceLocker()

	if err := os.MkdirAll(filepath.Dir(wl.lockFilePath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(wl.lockFilePath, []byte("999999\tstale"), 0o644); err != nil {
		t.Fatal(err)
	}

	ctx := context.Background()
	if err := wl.Lock(ctx); err != nil {
		t.Fatalf("Lock: %v", err)
	}
	_ = wl.Unlock()
}
