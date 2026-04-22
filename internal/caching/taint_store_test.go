package caching

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"grog/internal/config"
	"grog/internal/label"
)

func withIsolatedWorkspace(t *testing.T) string {
	t.Helper()
	tmp := t.TempDir()
	prev := config.Global
	config.Global = config.WorkspaceConfig{
		Root:          tmp,
		WorkspaceRoot: filepath.Join(tmp, "workspace"),
	}
	t.Cleanup(func() { config.Global = prev })
	return tmp
}

func TestTaintStore_RoundTrip(t *testing.T) {
	ctx := context.Background()
	withIsolatedWorkspace(t)
	store := NewTaintStore()

	lbl := label.TargetLabel{Package: "foo", Name: "bar"}

	tainted, err := store.IsTainted(ctx, lbl)
	if err != nil {
		t.Fatalf("IsTainted (pre): %v", err)
	}
	if tainted {
		t.Fatalf("target should not be tainted before Taint")
	}

	if err := store.Taint(ctx, lbl); err != nil {
		t.Fatalf("Taint: %v", err)
	}

	tainted, err = store.IsTainted(ctx, lbl)
	if err != nil {
		t.Fatalf("IsTainted (post): %v", err)
	}
	if !tainted {
		t.Fatalf("target should be tainted after Taint")
	}

	if err := store.Clear(ctx, lbl); err != nil {
		t.Fatalf("Clear: %v", err)
	}

	tainted, err = store.IsTainted(ctx, lbl)
	if err != nil {
		t.Fatalf("IsTainted (cleared): %v", err)
	}
	if tainted {
		t.Fatalf("target should not be tainted after Clear")
	}
}

func TestTaintStore_TaintIsIdempotent(t *testing.T) {
	ctx := context.Background()
	withIsolatedWorkspace(t)
	store := NewTaintStore()
	lbl := label.TargetLabel{Package: "foo", Name: "bar"}

	if err := store.Taint(ctx, lbl); err != nil {
		t.Fatalf("first Taint: %v", err)
	}
	if err := store.Taint(ctx, lbl); err != nil {
		t.Fatalf("second Taint: %v", err)
	}
}

func TestTaintStore_ClearOnUntaintedIsNoop(t *testing.T) {
	ctx := context.Background()
	withIsolatedWorkspace(t)
	store := NewTaintStore()
	lbl := label.TargetLabel{Package: "foo", Name: "bar"}

	if err := store.Clear(ctx, lbl); err != nil {
		t.Fatalf("Clear on untainted returned error: %v", err)
	}
}

func TestTaintStore_IsolatedAcrossWorkspaces(t *testing.T) {
	// Two different workspace checkouts that share the same $GROG_ROOT must
	// not see each other's taints. This is the regression that motivated
	// splitting TaintStore out of the shared cache backend.
	ctx := context.Background()
	root := t.TempDir()

	writeTaint := func(workspace string) *TaintStore {
		prev := config.Global
		t.Cleanup(func() { config.Global = prev })
		config.Global = config.WorkspaceConfig{Root: root, WorkspaceRoot: workspace}
		return NewTaintStore()
	}

	lbl := label.TargetLabel{Package: "foo", Name: "bar"}

	a := writeTaint(filepath.Join(root, "checkout-a"))
	if err := a.Taint(ctx, lbl); err != nil {
		t.Fatalf("Taint in A: %v", err)
	}

	b := writeTaint(filepath.Join(root, "checkout-b"))
	tainted, err := b.IsTainted(ctx, lbl)
	if err != nil {
		t.Fatalf("IsTainted in B: %v", err)
	}
	if tainted {
		t.Fatalf("taint bled across workspaces sharing GROG_ROOT")
	}
}

func TestTaintStore_LivesUnderWorkspaceRootDir(t *testing.T) {
	// Sanity: the taint dir must live under GetWorkspaceRootDir so that
	// `grog clean` (which wipes the workspace root dir) also clears taint.
	root := withIsolatedWorkspace(t)
	store := NewTaintStore()

	wantPrefix := config.Global.GetWorkspaceRootDir()
	if len(store.dir) < len(wantPrefix) || store.dir[:len(wantPrefix)] != wantPrefix {
		t.Fatalf("taint dir %q not under workspace root %q", store.dir, wantPrefix)
	}
	// And not under $GROG_ROOT/cache/ — that dir is shared.
	if cacheDir := filepath.Join(root, "cache"); len(store.dir) >= len(cacheDir) && store.dir[:len(cacheDir)] == cacheDir {
		t.Fatalf("taint dir %q must not live under the shared cache dir", store.dir)
	}
	// Directory doesn't need to exist until first Taint; verify we can
	// still Set / IsTainted / Clear without manually creating it.
	ctx := context.Background()
	lbl := label.TargetLabel{Package: "foo", Name: "bar"}
	if err := store.Taint(ctx, lbl); err != nil {
		t.Fatalf("Taint: %v", err)
	}
	if _, err := os.Stat(store.dir); err != nil {
		t.Fatalf("taint dir was not created: %v", err)
	}
}
