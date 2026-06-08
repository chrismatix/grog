package handlers

import (
	"path/filepath"
	"testing"

	"grog/internal/proto/gen"
)

func TestComputeDirectoryDigest(t *testing.T) {
	d := &gen.Directory{Files: []*gen.FileNode{{Name: "a"}}}
	dig, err := computeDirectoryDigest(d)
	if err != nil {
		t.Fatal(err)
	}
	if dig.Hash == "" {
		t.Fatal("empty hash")
	}
	if dig.SizeBytes == 0 {
		t.Fatal("zero size")
	}
}

func TestComputeFileDigest_Missing(t *testing.T) {
	if _, err := computeFileDigest(filepath.Join(t.TempDir(), "nope")); err == nil {
		t.Fatal("expected err")
	}
}

func TestStageFileSnapshot_Missing(t *testing.T) {
	if _, _, err := stageFileSnapshot(filepath.Join(t.TempDir(), "nope"), t.TempDir(), "x.txt"); err == nil {
		t.Fatal("expected err")
	}
}
