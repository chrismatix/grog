package locking

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

var _ = filepath.Dir

func TestWorkspaceLocker_Lock_BlocksWhenContended(t *testing.T) {
	setupTestWorkspace(t)
	wl := NewWorkspaceLocker()
	if err := wl.Lock(context.Background()); err != nil {
		t.Fatalf("first Lock: %v", err)
	}
	defer wl.Unlock()

	wl2 := NewWorkspaceLocker()
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	err := wl2.Lock(ctx)
	if err == nil {
		t.Fatal("expected timeout / ctx err")
	}
}

func TestWorkspaceLocker_Lock_LiveProcessWithCommand(t *testing.T) {
	setupTestWorkspace(t)
	wl := NewWorkspaceLocker()
	if err := os.MkdirAll(filepath.Dir(wl.lockFilePath), 0o755); err != nil {
		t.Fatal(err)
	}
	contents := buildLockFileContents(os.Getpid(), []string{"grog", "build", "//x"})
	if err := os.WriteFile(wl.lockFilePath, []byte(contents), 0o644); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if err := wl.Lock(ctx); err == nil {
		t.Fatal("expected timeout")
	}
}
