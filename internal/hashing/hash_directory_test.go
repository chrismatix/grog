package hashing

import (
	"os"
	"path/filepath"
	"testing"
)

func TestHashDirectory(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "hashdir_test")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	nested := filepath.Join(tempDir, "nested")
	if err := os.MkdirAll(nested, 0755); err != nil {
		t.Fatalf("failed to create nested dir: %v", err)
	}

	file1 := filepath.Join(tempDir, "a.txt")
	if err := os.WriteFile(file1, []byte("content a"), 0644); err != nil {
		t.Fatalf("failed to write file1: %v", err)
	}

	file2 := filepath.Join(nested, "b.txt")
	if err := os.WriteFile(file2, []byte("content b"), 0644); err != nil {
		t.Fatalf("failed to write file2: %v", err)
	}

	symlink := filepath.Join(tempDir, "link.txt")
	if err := os.Symlink("a.txt", symlink); err != nil {
		t.Fatalf("failed to create symlink: %v", err)
	}

	hash1, err := HashDirectory(tempDir)
	if err != nil {
		t.Fatalf("HashDirectory returned error: %v", err)
	}
	if hash1 == "" {
		t.Fatalf("expected hash to be non-empty")
	}

	// Modify a file and expect the hash to change
	if err := os.WriteFile(file2, []byte("content c"), 0644); err != nil {
		t.Fatalf("failed to rewrite file2: %v", err)
	}
	hash2, err := HashDirectory(tempDir)
	if err != nil {
		t.Fatalf("HashDirectory returned error: %v", err)
	}
	if hash1 == hash2 {
		t.Fatalf("expected hashes to differ when file content changes")
	}

	// Revert changes to ensure deterministic hashing
	if err := os.WriteFile(file2, []byte("content b"), 0644); err != nil {
		t.Fatalf("failed to rewrite file2: %v", err)
	}
	hash3, err := HashDirectory(tempDir)
	if err != nil {
		t.Fatalf("HashDirectory returned error: %v", err)
	}
	if hash3 != hash1 {
		t.Fatalf("expected hash to be stable after reverting changes")
	}
}
