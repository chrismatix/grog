package backends

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFileSystemCacheBuildFilePath(t *testing.T) {
	workspaceCacheDir := filepath.Join(t.TempDir(), "workspace-cache")
	sharedCasDir := filepath.Join(t.TempDir(), "shared-cas")
	cache := &FileSystemCache{
		workspaceCacheDir: workspaceCacheDir,
		sharedCasDir:      sharedCasDir,
	}

	tests := []struct {
		name string
		path string
		key  string
		want string
	}{
		{
			name: "workspace cache path includes cache namespace",
			path: "targets",
			key:  "abc123",
			want: filepath.Join(workspaceCacheDir, "targets", "abc123"),
		},
		{
			name: "cas path writes directly under shared cas directory",
			path: "cas",
			key:  "def456",
			want: filepath.Join(sharedCasDir, "def456"),
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			got := cache.buildFilePath(testCase.path, testCase.key)
			if got != testCase.want {
				t.Fatalf("buildFilePath(%q, %q) = %q, want %q", testCase.path, testCase.key, got, testCase.want)
			}
		})
	}
}

func TestFileSystemCacheSetForCasDoesNotDoubleNestCasDirectory(t *testing.T) {
	cache := &FileSystemCache{
		workspaceCacheDir: filepath.Join(t.TempDir(), "workspace-cache"),
		sharedCasDir:      filepath.Join(t.TempDir(), "shared-cas"),
	}

	const digest = "sha256:deadbeef"
	const content = "cached-content"

	if err := cache.Set(context.Background(), "cas", digest, strings.NewReader(content)); err != nil {
		t.Fatalf("Set failed: %v", err)
	}

	filePath := filepath.Join(cache.sharedCasDir, digest)
	fileBytes, err := os.ReadFile(filePath)
	if err != nil {
		t.Fatalf("failed to read cached file at %q: %v", filePath, err)
	}
	if string(fileBytes) != content {
		t.Fatalf("unexpected file content: got %q, want %q", string(fileBytes), content)
	}

	doubleNestedPath := filepath.Join(cache.sharedCasDir, "cas", digest)
	if _, err := os.Stat(doubleNestedPath); !os.IsNotExist(err) {
		t.Fatalf("expected no file at %q, got err=%v", doubleNestedPath, err)
	}

	reader, err := cache.Get(context.Background(), "cas", digest)
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}
	defer reader.Close()

	readContent, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("failed to read Get result: %v", err)
	}
	if string(readContent) != content {
		t.Fatalf("unexpected Get content: got %q, want %q", string(readContent), content)
	}
}
