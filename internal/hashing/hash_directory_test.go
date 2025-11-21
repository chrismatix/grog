package hashing

import (
	"os"
	"path/filepath"
	"testing"
)

// TestGetTree tests the HashDirectory function with various directory structures
func TestGetTree(t *testing.T) {
	t.Run("SimpleDirectory", func(t *testing.T) {
		// Create a temporary directory with simple structure
		tempDir, err := os.MkdirTemp("", "gettree_simple")
		if err != nil {
			t.Fatalf("Failed to create temp dir: %v", err)
		}
		defer os.RemoveAll(tempDir)

		// Create files
		if err := os.WriteFile(filepath.Join(tempDir, "file1.txt"), []byte("content1"), 0644); err != nil {
			t.Fatalf("Failed to create file1.txt: %v", err)
		}
		if err := os.WriteFile(filepath.Join(tempDir, "file2.txt"), []byte("content2"), 0755); err != nil {
			t.Fatalf("Failed to create file2.txt: %v", err)
		}

		tree, err := HashDirectory(tempDir)
		if err != nil {
			t.Fatalf("HashDirectory failed: %v", err)
		}

		if tree.Root == nil {
			t.Fatal("Expected root directory, got nil")
		}

		if len(tree.Root.Files) != 2 {
			t.Errorf("Expected 2 files, got %d", len(tree.Root.Files))
		}

		if len(tree.Root.Directories) != 0 {
			t.Errorf("Expected 0 directories, got %d", len(tree.Root.Directories))
		}

		if len(tree.Children) != 0 {
			t.Errorf("Expected 0 children, got %d", len(tree.Children))
		}

		// Check files are sorted
		if tree.Root.Files[0].Name != "file1.txt" || tree.Root.Files[1].Name != "file2.txt" {
			t.Errorf("Files not sorted correctly: %s, %s", tree.Root.Files[0].Name, tree.Root.Files[1].Name)
		}

		// Check executable flag
		if tree.Root.Files[0].IsExecutable {
			t.Errorf("file1.txt should not be executable")
		}
		if !tree.Root.Files[1].IsExecutable {
			t.Errorf("file2.txt should be executable")
		}

		// Check digests exist
		for i, file := range tree.Root.Files {
			if file.Digest == nil {
				t.Errorf("File %d has nil digest", i)
			} else if file.Digest.Hash == "" {
				t.Errorf("File %d has empty hash", i)
			}
		}
	})

	t.Run("NestedDirectories", func(t *testing.T) {
		// Create a temporary directory with nested structure
		tempDir, err := os.MkdirTemp("", "gettree_nested")
		if err != nil {
			t.Fatalf("Failed to create temp dir: %v", err)
		}
		defer os.RemoveAll(tempDir)

		// Create nested structure
		subDir1 := filepath.Join(tempDir, "subdir1")
		subDir2 := filepath.Join(tempDir, "subdir2")
		nestedDir := filepath.Join(subDir1, "nested")

		if err := os.MkdirAll(nestedDir, 0755); err != nil {
			t.Fatalf("Failed to create nested dir: %v", err)
		}
		if err := os.Mkdir(subDir2, 0755); err != nil {
			t.Fatalf("Failed to create subdir2: %v", err)
		}

		// Create files in various locations
		if err := os.WriteFile(filepath.Join(tempDir, "root.txt"), []byte("root content"), 0644); err != nil {
			t.Fatalf("Failed to create root.txt: %v", err)
		}
		if err := os.WriteFile(filepath.Join(subDir1, "sub1.txt"), []byte("sub1 content"), 0644); err != nil {
			t.Fatalf("Failed to create sub1.txt: %v", err)
		}
		if err := os.WriteFile(filepath.Join(nestedDir, "nested.txt"), []byte("nested content"), 0644); err != nil {
			t.Fatalf("Failed to create nested.txt: %v", err)
		}

		tree, err := HashDirectory(tempDir)
		if err != nil {
			t.Fatalf("HashDirectory failed: %v", err)
		}

		// Check root has 1 file and 2 directories
		if len(tree.Root.Files) != 1 {
			t.Errorf("Expected 1 file in root, got %d", len(tree.Root.Files))
		}
		if len(tree.Root.Directories) != 2 {
			t.Errorf("Expected 2 directories in root, got %d", len(tree.Root.Directories))
		}

		// Check children contains all subdirectories
		// subdir1, subdir2, and nested
		if len(tree.Children) != 3 {
			t.Errorf("Expected 3 children directories, got %d", len(tree.Children))
		}

		// Check directories are sorted
		if tree.Root.Directories[0].Name != "subdir1" || tree.Root.Directories[1].Name != "subdir2" {
			t.Errorf("Directories not sorted correctly: %s, %s",
				tree.Root.Directories[0].Name, tree.Root.Directories[1].Name)
		}

		// Check all directories have digests
		for i, dir := range tree.Root.Directories {
			if dir.Digest == nil {
				t.Errorf("Directory %d has nil digest", i)
			} else if dir.Digest.Hash == "" {
				t.Errorf("Directory %d has empty hash", i)
			}
		}
	})

	t.Run("WithSymlinks", func(t *testing.T) {
		// Create a temporary directory with symlinks
		tempDir, err := os.MkdirTemp("", "gettree_symlinks")
		if err != nil {
			t.Fatalf("Failed to create temp dir: %v", err)
		}
		defer os.RemoveAll(tempDir)

		// Create a file and a symlink to it
		targetFile := filepath.Join(tempDir, "target.txt")
		if err := os.WriteFile(targetFile, []byte("target content"), 0644); err != nil {
			t.Fatalf("Failed to create target.txt: %v", err)
		}

		symlinkPath := filepath.Join(tempDir, "link.txt")
		if err := os.Symlink("target.txt", symlinkPath); err != nil {
			t.Fatalf("Failed to create symlink: %v", err)
		}

		tree, err := HashDirectory(tempDir)
		if err != nil {
			t.Fatalf("HashDirectory failed: %v", err)
		}

		// Check we have 1 file and 1 symlink
		if len(tree.Root.Files) != 1 {
			t.Errorf("Expected 1 file, got %d", len(tree.Root.Files))
		}
		if len(tree.Root.Symlinks) != 1 {
			t.Errorf("Expected 1 symlink, got %d", len(tree.Root.Symlinks))
		}

		// Check symlink properties
		symlink := tree.Root.Symlinks[0]
		if symlink.Name != "link.txt" {
			t.Errorf("Expected symlink name 'link.txt', got %s", symlink.Name)
		}
		if symlink.Target != "target.txt" {
			t.Errorf("Expected symlink target 'target.txt', got %s", symlink.Target)
		}
	})

	t.Run("EmptyDirectory", func(t *testing.T) {
		// Create an empty temporary directory
		tempDir, err := os.MkdirTemp("", "gettree_empty")
		if err != nil {
			t.Fatalf("Failed to create temp dir: %v", err)
		}
		defer os.RemoveAll(tempDir)

		tree, err := HashDirectory(tempDir)
		if err != nil {
			t.Fatalf("HashDirectory failed: %v", err)
		}

		if tree.Root == nil {
			t.Fatal("Expected root directory, got nil")
		}

		if len(tree.Root.Files) != 0 {
			t.Errorf("Expected 0 files, got %d", len(tree.Root.Files))
		}
		if len(tree.Root.Directories) != 0 {
			t.Errorf("Expected 0 directories, got %d", len(tree.Root.Directories))
		}
		if len(tree.Children) != 0 {
			t.Errorf("Expected 0 children, got %d", len(tree.Children))
		}
	})

	t.Run("NonExistentDirectory", func(t *testing.T) {
		_, err := HashDirectory("/nonexistent/path/that/does/not/exist")
		if err == nil {
			t.Error("Expected error for non-existent directory, got nil")
		}
	})

	t.Run("DeterministicHashing", func(t *testing.T) {
		// Create a temporary directory
		tempDir, err := os.MkdirTemp("", "gettree_deterministic")
		if err != nil {
			t.Fatalf("Failed to create temp dir: %v", err)
		}
		defer os.RemoveAll(tempDir)

		// Create files
		if err := os.WriteFile(filepath.Join(tempDir, "file1.txt"), []byte("content1"), 0644); err != nil {
			t.Fatalf("Failed to create file1.txt: %v", err)
		}
		if err := os.WriteFile(filepath.Join(tempDir, "file2.txt"), []byte("content2"), 0644); err != nil {
			t.Fatalf("Failed to create file2.txt: %v", err)
		}

		// Get tree twice
		tree1, err := HashDirectory(tempDir)
		if err != nil {
			t.Fatalf("HashDirectory failed: %v", err)
		}

		tree2, err := HashDirectory(tempDir)
		if err != nil {
			t.Fatalf("HashDirectory failed: %v", err)
		}

		// Check that hashes are the same
		if len(tree1.Root.Files) != len(tree2.Root.Files) {
			t.Fatal("Different number of files in two runs")
		}

		for i := range tree1.Root.Files {
			if tree1.Root.Files[i].Digest.Hash != tree2.Root.Files[i].Digest.Hash {
				t.Errorf("File %d has different hash: %s vs %s",
					i, tree1.Root.Files[i].Digest.Hash, tree2.Root.Files[i].Digest.Hash)
			}
		}
	})
}

// TestComputeFileDigest tests the computeFileDigest function
func TestComputeFileDigest(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "filedigest_test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	testFile := filepath.Join(tempDir, "test.txt")
	content := []byte("Hello, world!")
	if err := os.WriteFile(testFile, content, 0644); err != nil {
		t.Fatalf("Failed to write test file: %v", err)
	}

	digest, err := computeFileDigest(testFile)
	if err != nil {
		t.Fatalf("computeFileDigest failed: %v", err)
	}

	if digest == nil {
		t.Fatal("Expected digest, got nil")
	}

	if digest.Hash == "" {
		t.Error("Expected non-empty hash")
	}

	if digest.SizeBytes != int64(len(content)) {
		t.Errorf("Expected size %d, got %d", len(content), digest.SizeBytes)
	}

	// Test with known hash value
	expectedHash := "f58336a78b6f9476" // xxhash of "Hello, world!"
	if digest.Hash != expectedHash {
		t.Errorf("Expected hash %s, got %s", expectedHash, digest.Hash)
	}
}

// TestComputeDirectoryDigest tests the computeDirectoryDigest function
func TestComputeDirectoryDigest(t *testing.T) {
	// Create a simple directory structure
	tempDir, err := os.MkdirTemp("", "dirdigest_test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	if err := os.WriteFile(filepath.Join(tempDir, "file.txt"), []byte("content"), 0644); err != nil {
		t.Fatalf("Failed to write test file: %v", err)
	}

	tree, err := HashDirectory(tempDir)
	if err != nil {
		t.Fatalf("HashDirectory failed: %v", err)
	}

	// Compute digest of root directory
	digest, err := computeDirectoryDigest(tree.Root)
	if err != nil {
		t.Fatalf("computeDirectoryDigest failed: %v", err)
	}

	if digest == nil {
		t.Fatal("Expected digest, got nil")
	}

	if digest.Hash == "" {
		t.Error("Expected non-empty hash")
	}

	if digest.SizeBytes <= 0 {
		t.Errorf("Expected positive size, got %d", digest.SizeBytes)
	}

	// Test determinism - same directory should produce same hash
	digest2, err := computeDirectoryDigest(tree.Root)
	if err != nil {
		t.Fatalf("computeDirectoryDigest failed on second call: %v", err)
	}

	if digest.Hash != digest2.Hash {
		t.Errorf("Expected same hash, got %s and %s", digest.Hash, digest2.Hash)
	}
}
