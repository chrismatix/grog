package backends

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFileSystemCache_SetGetExistsDelete(t *testing.T) {
	contextBackground := context.Background()

	testCases := []struct {
		name    string
		path    string
		key     string
		content string
	}{
		{
			name:    "simple key",
			path:    "taint",
			key:     "target",
			content: "content-1",
		},
		{
			name:    "label key with path separators",
			path:    "taint",
			key:     "//dbt/container:clickhouse-dbt-arm64",
			content: "content-2",
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			fileSystemCache := &FileSystemCache{
				workspaceCacheDir: t.TempDir(),
				workspaceTaintDir: t.TempDir(),
				sharedCasDir:      t.TempDir(),
			}

			err := fileSystemCache.Set(
				contextBackground,
				testCase.path,
				testCase.key,
				strings.NewReader(testCase.content),
			)
			if err != nil {
				t.Fatalf("Set returned error: %v", err)
			}

			exists, err := fileSystemCache.Exists(contextBackground, testCase.path, testCase.key)
			if err != nil {
				t.Fatalf("Exists returned error: %v", err)
			}
			if !exists {
				t.Fatalf("Exists returned false for key %q", testCase.key)
			}

			reader, err := fileSystemCache.Get(contextBackground, testCase.path, testCase.key)
			if err != nil {
				t.Fatalf("Get returned error: %v", err)
			}
			defer reader.Close()

			contentBytes, err := io.ReadAll(reader)
			if err != nil {
				t.Fatalf("failed reading cached content: %v", err)
			}
			if string(contentBytes) != testCase.content {
				t.Fatalf("expected %q, got %q", testCase.content, string(contentBytes))
			}

			err = fileSystemCache.Delete(contextBackground, testCase.path, testCase.key)
			if err != nil {
				t.Fatalf("Delete returned error: %v", err)
			}

			exists, err = fileSystemCache.Exists(contextBackground, testCase.path, testCase.key)
			if err != nil {
				t.Fatalf("Exists after Delete returned error: %v", err)
			}
			if exists {
				t.Fatalf("expected key %q to be deleted", testCase.key)
			}
		})
	}
}

func TestFileSystemCacheBuildFilePath(t *testing.T) {
	workspaceCacheDir := filepath.Join(t.TempDir(), "workspace-cache")
	workspaceTaintDir := filepath.Join(t.TempDir(), "workspace-taint")
	sharedCasDir := filepath.Join(t.TempDir(), "shared-cas")
	cache := &FileSystemCache{
		workspaceCacheDir: workspaceCacheDir,
		workspaceTaintDir: workspaceTaintDir,
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
		{
			name: "taint path writes under the workspace-local taint directory",
			path: "taint",
			key:  "//foo:bar",
			want: filepath.Join(workspaceTaintDir, "//foo:bar"),
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
		workspaceTaintDir: filepath.Join(t.TempDir(), "workspace-taint"),
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

// TestFileSystemCache_BeginWriteCommit verifies that bytes streamed through a
// staged writer end up at the final cache key after Commit and that the
// staging directory is left empty (the staging file was renamed, not copied).
func TestFileSystemCache_BeginWriteCommit(t *testing.T) {
	ctx := context.Background()
	sharedCasDir := t.TempDir()
	cache := &FileSystemCache{
		workspaceCacheDir: t.TempDir(),
		workspaceTaintDir: t.TempDir(),
		sharedCasDir:      sharedCasDir,
	}

	sw, err := cache.BeginWrite(ctx)
	if err != nil {
		t.Fatalf("BeginWrite failed: %v", err)
	}

	payload := []byte("staged write content")
	if _, err := sw.Write(payload); err != nil {
		t.Fatalf("Write failed: %v", err)
	}
	if err := sw.Commit(ctx, "cas", "sha256:cafef00d"); err != nil {
		t.Fatalf("Commit failed: %v", err)
	}

	// Read it back via the normal CAS path.
	reader, err := cache.Get(ctx, "cas", "sha256:cafef00d")
	if err != nil {
		t.Fatalf("Get after commit failed: %v", err)
	}
	defer reader.Close()
	got, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("read after commit: %v", err)
	}
	if string(got) != string(payload) {
		t.Fatalf("commit content mismatch: got %q want %q", got, payload)
	}

	// The staging directory must be empty — the file was renamed, not copied.
	stagingEntries, err := os.ReadDir(filepath.Join(sharedCasDir, fsStagingDirName))
	if err != nil {
		t.Fatalf("read staging dir: %v", err)
	}
	if len(stagingEntries) != 0 {
		t.Fatalf("expected staging dir to be empty after commit, got %d entries", len(stagingEntries))
	}
}

// TestFileSystemCache_BeginWriteCancel verifies that Cancel removes the
// staged file and never makes it visible at any cache key.
func TestFileSystemCache_BeginWriteCancel(t *testing.T) {
	ctx := context.Background()
	sharedCasDir := t.TempDir()
	cache := &FileSystemCache{
		workspaceCacheDir: t.TempDir(),
		workspaceTaintDir: t.TempDir(),
		sharedCasDir:      sharedCasDir,
	}

	sw, err := cache.BeginWrite(ctx)
	if err != nil {
		t.Fatalf("BeginWrite failed: %v", err)
	}
	if _, err := sw.Write([]byte("partial garbage")); err != nil {
		t.Fatalf("Write failed: %v", err)
	}
	if err := sw.Cancel(ctx); err != nil {
		t.Fatalf("Cancel failed: %v", err)
	}

	// Cancel must be idempotent.
	if err := sw.Cancel(ctx); err != nil {
		t.Fatalf("second Cancel returned error: %v", err)
	}

	// The staging dir should be empty.
	stagingEntries, err := os.ReadDir(filepath.Join(sharedCasDir, fsStagingDirName))
	if err != nil {
		t.Fatalf("read staging dir: %v", err)
	}
	if len(stagingEntries) != 0 {
		t.Fatalf("expected staging dir to be empty after cancel, got %d entries", len(stagingEntries))
	}

	// Writes after Cancel must be rejected.
	if _, err := sw.Write([]byte("after cancel")); err == nil {
		t.Fatalf("expected Write after Cancel to fail")
	}
}

// TestFileSystemCache_BeginWriteListKeysFiltersStaging verifies that
// in-flight uploads under .staging/ never leak into ListKeys output. Stale
// staging files (e.g. from a crashed previous run) must look invisible to
// cache enumeration.
func TestFileSystemCache_BeginWriteListKeysFiltersStaging(t *testing.T) {
	ctx := context.Background()
	sharedCasDir := t.TempDir()
	cache := &FileSystemCache{
		workspaceCacheDir: t.TempDir(),
		workspaceTaintDir: t.TempDir(),
		sharedCasDir:      sharedCasDir,
	}

	// Drop a real cache entry alongside a leaked staging file.
	if err := cache.Set(ctx, "cas", "sha256:committed", strings.NewReader("ok")); err != nil {
		t.Fatalf("Set: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(sharedCasDir, fsStagingDirName), 0755); err != nil {
		t.Fatalf("mkdir staging: %v", err)
	}
	if err := os.WriteFile(filepath.Join(sharedCasDir, fsStagingDirName, "leaked-upload"), []byte("dangling"), 0644); err != nil {
		t.Fatalf("write leaked staging file: %v", err)
	}

	keys, err := cache.ListKeys(ctx, "cas", "")
	if err != nil {
		t.Fatalf("ListKeys: %v", err)
	}

	for _, k := range keys {
		if strings.Contains(k, fsStagingDirName) {
			t.Fatalf("ListKeys leaked staging entry: %q", k)
		}
	}

	// Sanity check: the committed entry is still discoverable.
	var found bool
	for _, k := range keys {
		if k == "sha256:committed" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("ListKeys missing committed entry; got %v", keys)
	}
}

// TestFileSystemCache_BeginWriteCommitErrorOnDoubleCommit guards against
// callers trying to reuse a staged writer after Commit/Cancel.
func TestFileSystemCache_BeginWriteCommitErrorOnDoubleCommit(t *testing.T) {
	ctx := context.Background()
	cache := &FileSystemCache{
		workspaceCacheDir: t.TempDir(),
		workspaceTaintDir: t.TempDir(),
		sharedCasDir:      t.TempDir(),
	}

	sw, err := cache.BeginWrite(ctx)
	if err != nil {
		t.Fatalf("BeginWrite: %v", err)
	}
	if _, err := sw.Write([]byte("data")); err != nil {
		t.Fatalf("Write: %v", err)
	}
	if err := sw.Commit(ctx, "cas", "sha256:abc"); err != nil {
		t.Fatalf("first Commit: %v", err)
	}
	if err := sw.Commit(ctx, "cas", "sha256:def"); err == nil {
		t.Fatalf("expected second Commit to fail")
	}
	// Cancel after Commit should be a no-op (no error).
	if err := sw.Cancel(ctx); err != nil {
		t.Fatalf("Cancel after Commit: %v", err)
	}
	// And Write should fail.
	if _, err := sw.Write([]byte("more")); err == nil || !errors.Is(err, err) {
		t.Fatalf("expected Write after Commit to fail; got %v", err)
	}
}
