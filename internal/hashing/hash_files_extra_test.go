package hashing

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestHashFiles_PermissionError(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("permission semantics differ")
	}
	dir := t.TempDir()
	bad := filepath.Join(dir, "bad.txt")
	if err := os.WriteFile(bad, []byte("x"), 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(bad, 0o644) })

	if os.Geteuid() == 0 {
		t.Skip("running as root bypasses perm bits")
	}
	if _, err := HashFiles(dir, []string{"bad.txt"}); err == nil {
		t.Fatal("expected err")
	}
}

func TestHashFile_OK(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "x.txt")
	if err := os.WriteFile(p, []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	h, err := HashFile(p)
	if err != nil {
		t.Fatal(err)
	}
	if h == "" {
		t.Fatal("empty hash")
	}
}
