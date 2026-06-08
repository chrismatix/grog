package loading

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"grog/internal/config"
	"grog/internal/console"
)

func setupGraphWorkspace(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "BUILD.json"),
		[]byte(`{"targets":[{"name":"t1","command":"echo a"}]}`), 0644); err != nil {
		t.Fatal(err)
	}

	originalRoot := config.Global.WorkspaceRoot
	originalDet := config.Global.DisableNonDeterministicLogging
	config.Global.WorkspaceRoot = dir
	t.Cleanup(func() {
		config.Global.WorkspaceRoot = originalRoot
		config.Global.DisableNonDeterministicLogging = originalDet
	})
}

func TestMustLoadGraphForBuild_HappyPath(t *testing.T) {
	setupGraphWorkspace(t)
	logger := newTestLogger(t)
	ctx := console.WithLogger(context.Background(), logger)
	g := MustLoadGraphForBuild(ctx, logger)
	if g == nil {
		t.Fatal("expected non-nil graph")
	}
}

func TestMustLoadGraphForBuild_Deterministic(t *testing.T) {
	setupGraphWorkspace(t)
	config.Global.DisableNonDeterministicLogging = true
	logger := newTestLogger(t)
	ctx := console.WithLogger(context.Background(), logger)
	g := MustLoadGraphForBuild(ctx, logger)
	if g == nil {
		t.Fatal("expected non-nil graph")
	}
}

func TestMustLoadGraphForQuery_HappyPath(t *testing.T) {
	setupGraphWorkspace(t)
	logger := newTestLogger(t)
	ctx := console.WithLogger(context.Background(), logger)
	g := MustLoadGraphForQuery(ctx, logger)
	if g == nil {
		t.Fatal("expected non-nil graph")
	}
}
