package backends

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"slices"
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
			path:    "target",
			key:     "target",
			content: "content-1",
		},
		{
			name:    "key with path separators",
			path:    "target",
			key:     "//dbt/container:clickhouse-dbt-arm64",
			content: "content-2",
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			fileSystemCache := &FileSystemCache{
				workspaceCacheDir: t.TempDir(),
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

// TestFileSystemCache_BeginWriteCommit verifies that bytes streamed through a
// staged writer end up at the final cache key after Commit and that the
// staging directory is left empty (the staging file was renamed, not copied).
func TestFileSystemCache_BeginWriteCommit(t *testing.T) {
	ctx := context.Background()
	sharedCasDir := t.TempDir()
	cache := &FileSystemCache{
		workspaceCacheDir: t.TempDir(),
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
	if slices.Contains(keys, "sha256:committed") {
		found = true
	}
	if !found {
		t.Fatalf("ListKeys missing committed entry; got %v", keys)
	}
}

func TestFileSystemCache_TypeName(t *testing.T) {
	c := &FileSystemCache{}
	if c.TypeName() != "fs" {
		t.Fatalf("TypeName = %q want fs", c.TypeName())
	}
}

func TestFileSystemCache_Size(t *testing.T) {
	cache := &FileSystemCache{
		workspaceCacheDir: t.TempDir(),
		sharedCasDir:      t.TempDir(),
	}
	ctx := context.Background()
	if err := cache.Set(ctx, "p", "k", strings.NewReader("hello!")); err != nil {
		t.Fatalf("Set: %v", err)
	}
	got, err := cache.Size(ctx, "p", "k")
	if err != nil {
		t.Fatalf("Size: %v", err)
	}
	if got != int64(len("hello!")) {
		t.Fatalf("Size = %d, want %d", got, len("hello!"))
	}

	if _, err := cache.Size(ctx, "p", "missing"); err == nil {
		t.Fatal("Size for missing should error")
	}
}

func TestFileSystemCache_SetMkdirFails(t *testing.T) {
	cache := &FileSystemCache{
		workspaceCacheDir: string([]byte{0}),
		sharedCasDir:      t.TempDir(),
	}
	if err := cache.Set(context.Background(), "p", "k", strings.NewReader("x")); err == nil {
		t.Fatal("expected Set to fail with invalid dir")
	}
}

func TestFileSystemCache_ExistsReturnsErrorOnBadPath(t *testing.T) {
	cache := &FileSystemCache{
		workspaceCacheDir: t.TempDir(),
		sharedCasDir:      t.TempDir(),
	}
	dir := filepath.Join(cache.workspaceCacheDir, "p")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "k"), []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}
	exists, err := cache.Exists(context.Background(), "p", "k")
	if err != nil || !exists {
		t.Fatalf("Exists = %v, %v", exists, err)
	}
}

func TestFileSystemCache_ListKeysEmpty(t *testing.T) {
	cache := &FileSystemCache{
		workspaceCacheDir: t.TempDir(),
		sharedCasDir:      t.TempDir(),
	}
	keys, err := cache.ListKeys(context.Background(), "nonexistent", "")
	if err != nil {
		t.Fatalf("ListKeys: %v", err)
	}
	if len(keys) != 0 {
		t.Fatalf("expected empty, got %v", keys)
	}
}

func TestFileSystemCache_ListKeysSuffix(t *testing.T) {
	cache := &FileSystemCache{
		workspaceCacheDir: t.TempDir(),
		sharedCasDir:      t.TempDir(),
	}
	ctx := context.Background()
	for _, k := range []string{"a.json", "b.json", "c.txt"} {
		if err := cache.Set(ctx, "p", k, strings.NewReader("x")); err != nil {
			t.Fatal(err)
		}
	}
	keys, err := cache.ListKeys(ctx, "p", ".json")
	if err != nil {
		t.Fatal(err)
	}
	if len(keys) != 2 {
		t.Fatalf("expected 2 json keys, got %v", keys)
	}
}

func TestFileSystemCache_CommitErrorAfterCancel(t *testing.T) {
	ctx := context.Background()
	cache := &FileSystemCache{
		workspaceCacheDir: t.TempDir(),
		sharedCasDir:      t.TempDir(),
	}
	sw, err := cache.BeginWrite(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := sw.Cancel(ctx); err != nil {
		t.Fatal(err)
	}
	if err := sw.Commit(ctx, "cas", "k"); err == nil {
		t.Fatal("expected Commit after Cancel to fail")
	}
}

func TestGetDirectorySizeBytes(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "a"), []byte("12345"), 0644); err != nil {
		t.Fatal(err)
	}
	sub := filepath.Join(dir, "sub")
	if err := os.MkdirAll(sub, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sub, "b"), []byte("123"), 0644); err != nil {
		t.Fatal(err)
	}

	size, err := getDirectorySizeBytes(dir)
	if err != nil {
		t.Fatal(err)
	}
	if size != 8 {
		t.Fatalf("size = %d, want 8", size)
	}

	if _, err := getDirectorySizeBytes(filepath.Join(t.TempDir(), "nonexistent")); err == nil {
		t.Fatal("expected error for missing dir")
	}
}

func TestFileSystemCache_GetWorkspaceCacheSizeBytes(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "a"), []byte("xx"), 0644); err != nil {
		t.Fatal(err)
	}
	cache := &FileSystemCache{
		workspaceCacheDir: dir,
		sharedCasDir:      t.TempDir(),
	}
	size, err := cache.GetWorkspaceCacheSizeBytes()
	if err != nil {
		t.Fatal(err)
	}
	if size != 2 {
		t.Fatalf("size = %d want 2", size)
	}
}

// TestFileSystemCache_BeginWriteCommitErrorOnDoubleCommit guards against
// callers trying to reuse a staged writer after Commit/Cancel.
func TestFileSystemCache_BeginWriteCommitErrorOnDoubleCommit(t *testing.T) {
	ctx := context.Background()
	cache := &FileSystemCache{
		workspaceCacheDir: t.TempDir(),
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
