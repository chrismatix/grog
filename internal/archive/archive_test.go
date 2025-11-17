package archive_test

import (
	"bytes"
	"context"
	"grog/internal/archive"
	"os"
	"path/filepath"
	"testing"
)

func TestTarGzipDirectoryAndExtractTarGzip(t *testing.T) {
	ctx := context.Background()

	srcDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(srcDir, "file.txt"), []byte("content"), 0o644); err != nil {
		t.Fatalf("failed to write file: %v", err)
	}

	nestedDir := filepath.Join(srcDir, "nested")
	if err := os.MkdirAll(nestedDir, 0o755); err != nil {
		t.Fatalf("failed to create nested dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(nestedDir, "nested.txt"), []byte("nested"), 0o644); err != nil {
		t.Fatalf("failed to write nested file: %v", err)
	}

	if err := os.Symlink("file.txt", filepath.Join(srcDir, "link")); err != nil {
		t.Fatalf("failed to create symlink: %v", err)
	}

	var buf bytes.Buffer
	if err := archive.TarGzipDirectory(ctx, srcDir, &buf); err != nil {
		t.Fatalf("TarGzipDirectory failed: %v", err)
	}

	destDir := filepath.Join(t.TempDir(), "restore")
	count, err := archive.ExtractTarGzip(ctx, bytes.NewReader(buf.Bytes()), destDir)
	if err != nil {
		t.Fatalf("ExtractTarGzip failed: %v", err)
	}

	if count != 4 { // nested dir + two files + symlink
		t.Fatalf("unexpected entry count: %d", count)
	}

	restored, err := os.ReadFile(filepath.Join(destDir, "file.txt"))
	if err != nil {
		t.Fatalf("failed to read restored file: %v", err)
	}
	if string(restored) != "content" {
		t.Fatalf("restored content mismatch: %s", restored)
	}

	nested, err := os.ReadFile(filepath.Join(destDir, "nested", "nested.txt"))
	if err != nil {
		t.Fatalf("failed to read nested file: %v", err)
	}
	if string(nested) != "nested" {
		t.Fatalf("nested content mismatch: %s", nested)
	}

	linkTarget, err := os.Readlink(filepath.Join(destDir, "link"))
	if err != nil {
		t.Fatalf("failed to read restored symlink: %v", err)
	}
	if linkTarget != "file.txt" {
		t.Fatalf("symlink target mismatch: %s", linkTarget)
	}
}
