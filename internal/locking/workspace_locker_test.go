package locking

import (
	"context"
	"errors"
	"grog/internal/config"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"
)

func setupTestWorkspace(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	config.Global.Root = root
	config.Global.WorkspaceRoot = root
	dir := config.Global.GetWorkspaceRootDir()
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatalf("failed to create workspace dir: %v", err)
	}
	return filepath.Join(dir, "lockfile")
}

func TestWorkspaceLockerBasic(t *testing.T) {
	lockPath := setupTestWorkspace(t)

	locker := NewWorkspaceLocker()
	ctx := context.Background()
	if err := locker.Lock(ctx); err != nil {
		t.Fatalf("lock failed: %v", err)
	}
	if _, err := os.Stat(lockPath); err != nil {
		t.Fatalf("expected lock file: %v", err)
	}
	if err := locker.Unlock(); err != nil {
		t.Fatalf("unlock failed: %v", err)
	}
	if _, err := os.Stat(lockPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("lock file still exists")
	}
}

func TestWorkspaceLockerWaitsAndReleases(t *testing.T) {
	lockPath := setupTestWorkspace(t)

	locker1 := NewWorkspaceLocker()
	ctx := context.Background()
	if err := locker1.Lock(ctx); err != nil {
		t.Fatalf("first lock failed: %v", err)
	}

	done := make(chan struct{})
	go func() {
		locker2 := NewWorkspaceLocker()
		if err := locker2.Lock(ctx); err != nil {
			t.Errorf("second lock failed: %v", err)
		}
		close(done)
	}()

	time.Sleep(150 * time.Millisecond)
	select {
	case <-done:
		t.Fatal("second lock acquired while first still held")
	default:
	}

	if err := locker1.Unlock(); err != nil {
		t.Fatalf("unlock failed: %v", err)
	}

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("second locker did not acquire after release")
	}

	if _, err := os.Stat(lockPath); err != nil {
		t.Fatalf("expected lock file after second lock: %v", err)
	}
}

func TestWorkspaceLockerStaleFile(t *testing.T) {
	lockPath := setupTestWorkspace(t)
	if err := os.WriteFile(lockPath, []byte("999999"), 0644); err != nil {
		t.Fatalf("failed to write stale lock file: %v", err)
	}

	locker := NewWorkspaceLocker()
	ctx := context.Background()
	if err := locker.Lock(ctx); err != nil {
		t.Fatalf("lock failed: %v", err)
	}

	data, err := os.ReadFile(lockPath)
	if err != nil {
		t.Fatalf("failed reading lockfile: %v", err)
	}
	pidStr := string(data)
	if pidStr != strconv.Itoa(os.Getpid()) {
		t.Fatalf("unexpected pid in lockfile: %s", pidStr)
	}
}

func TestWorkspaceLockerCancellation(t *testing.T) {
	lockPath := setupTestWorkspace(t)

	locker1 := NewWorkspaceLocker()
	ctx := context.Background()
	if err := locker1.Lock(ctx); err != nil {
		t.Fatalf("first lock failed: %v", err)
	}

	ctx2, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() {
		locker2 := NewWorkspaceLocker()
		errCh <- locker2.Lock(ctx2)
	}()

	time.Sleep(150 * time.Millisecond)
	cancel()

	select {
	case err := <-errCh:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("expected context canceled error, got %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("lock did not unblock after context cancellation")
	}

	if err := locker1.Unlock(); err != nil {
		t.Fatalf("unlock failed: %v", err)
	}

	if _, err := os.Stat(lockPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("lock file still exists after unlock")
	}
}
