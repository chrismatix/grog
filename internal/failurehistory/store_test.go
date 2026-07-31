package failurehistory

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"grog/internal/caching/backends"
	"grog/internal/config"
	"grog/internal/label"
	"grog/internal/model"
)

func TestStoreReportsChangesSinceLastGreen(t *testing.T) {
	workspaceRoot := t.TempDir()
	previousConfig := config.Global
	config.Global = config.WorkspaceConfig{
		Root:          t.TempDir(),
		WorkspaceRoot: workspaceRoot,
	}
	t.Cleanup(func() {
		config.Global = previousConfig
	})

	writeTestFile(t, filepath.Join(workspaceRoot, "BUILD.json"), "{}")
	writeTestFile(t, filepath.Join(workspaceRoot, "first.txt"), "first")
	writeTestFile(t, filepath.Join(workspaceRoot, "removed.txt"), "removed")

	target := &model.Target{
		Label:          label.TargetLabel{Name: "test"},
		SourceFilePath: filepath.Join(workspaceRoot, "BUILD.json"),
		Inputs:         []string{"first.txt", "removed.txt"},
	}
	backend, err := backends.NewFileSystemCache(context.Background())
	if err != nil {
		t.Fatalf("could not create cache: %v", err)
	}
	store := NewStore(backend)
	if err := store.Save(context.Background(), target); err != nil {
		t.Fatalf("could not save manifest: %v", err)
	}

	writeTestFile(t, filepath.Join(workspaceRoot, "first.txt"), "changed")
	writeTestFile(t, filepath.Join(workspaceRoot, "added.txt"), "added")
	target.Inputs = []string{"first.txt", "added.txt"}

	changes, err := store.Changes(context.Background(), target)
	if err != nil {
		t.Fatalf("could not compare manifests: %v", err)
	}

	expected := []InputChange{
		{Path: "added.txt", Kind: "added"},
		{Path: "first.txt", Kind: "modified"},
		{Path: "removed.txt", Kind: "removed"},
	}
	if !slices.Equal(changes, expected) {
		t.Errorf("expected %v, got %v", expected, changes)
	}
}

func writeTestFile(t *testing.T, path string, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("could not write %s: %v", path, err)
	}
}
