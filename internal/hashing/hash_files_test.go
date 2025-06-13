package hashing

import (
	"os"
	"path/filepath"
	"testing"
)

// TestHashFile tests the HashFile function with various scenarios
func TestHashFile(t *testing.T) {
	// Create a temporary directory for test files
	tempDir, err := os.MkdirTemp("", "hashfile_test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	// Create a test file with known content
	testFile := filepath.Join(tempDir, "test.txt")
	content := []byte("Hello, world!")
	if err := os.WriteFile(testFile, content, 0644); err != nil {
		t.Fatalf("Failed to write test file: %v", err)
	}

	// Test successful hash computation
	t.Run("ValidFile", func(t *testing.T) {
		hash, err := HashFile(testFile)
		if err != nil {
			t.Fatalf("HashFile returned error: %v", err)
		}
		// xxHash of "Hello, world!" should be consistent
		expectedHash := "f58336a78b6f9476"
		if hash != expectedHash {
			t.Errorf("Expected hash %s, got %s", expectedHash, hash)
		}
	})

	// Test with non-existent file
	t.Run("NonExistentFile", func(t *testing.T) {
		nonExistentFile := filepath.Join(tempDir, "nonexistent.txt")
		_, err := HashFile(nonExistentFile)
		if err == nil {
			t.Errorf("Expected error for non-existent file, got nil")
		}
	})
}

// TestHashFiles tests the HashFiles function
func TestHashFiles(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "hashfiles_test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	// Create multiple test files with known content
	testFiles := []struct {
		name    string
		content string
	}{
		{"file1.txt", "Content of file 1"},
		{"file2.txt", "Content of file 2"},
		{"file3.txt", "Content of file 3"},
	}

	var fileList []string
	for _, tf := range testFiles {
		filePath := filepath.Join(tempDir, tf.name)
		if err := os.WriteFile(filePath, []byte(tf.content), 0644); err != nil {
			t.Fatalf("Failed to write test file %s: %v", tf.name, err)
		}
		fileList = append(fileList, tf.name)
	}

	// Test successful hash computation for multiple files
	t.Run("ValidFiles", func(t *testing.T) {
		hash, err := HashFiles(tempDir, fileList)
		if err != nil {
			t.Fatalf("HashFiles returned error: %v", err)
		}

		// The combined hash should be deterministic
		// We can verify it's not empty
		expectedHash := "66daafd15e5df82a"
		if hash != expectedHash {
			t.Errorf("Expected hash %s, got %s", expectedHash, hash)
		}
	})

	// Test with a non-existent file in the list
	t.Run("NonExistentFileInList", func(t *testing.T) {
		invalidList := append(fileList, "nonexistent.txt")
		_, err := HashFiles(tempDir, invalidList)
		if err != nil {
			t.Errorf("non-existent file in list should not error, got %s", err)
		}
	})

	// Test with file order independence (hash should be the same regardless of file order)
	t.Run("FileOrderIndependence", func(t *testing.T) {
		hash1, err := HashFiles(tempDir, []string{"file1.txt", "file2.txt", "file3.txt"})
		if err != nil {
			t.Fatalf("HashFiles returned error: %v", err)
		}

		// Different order
		hash2, err := HashFiles(tempDir, []string{"file3.txt", "file1.txt", "file2.txt"})
		if err != nil {
			t.Fatalf("HashFiles returned error: %v", err)
		}

		// Should produce the same hash due to sorting in the implementation
		if hash1 != hash2 {
			t.Errorf("Hash values differ: %s vs %s", hash1, hash2)
		}
	})
}
