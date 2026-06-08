package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMustFindWorkspaceRoot_Success(t *testing.T) {
	tmp := t.TempDir()
	resolved, err := filepath.EvalSymlinks(tmp)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(resolved, "grog.toml"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	sub := filepath.Join(resolved, "a", "b")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	cwd, _ := os.Getwd()
	t.Cleanup(func() { _ = os.Chdir(cwd) })
	if err := os.Chdir(sub); err != nil {
		t.Fatal(err)
	}
	if got := MustFindWorkspaceRoot(); got != resolved {
		t.Fatalf("got %q want %q", got, resolved)
	}
}

func TestGetPathRelativeToWorkspaceRoot(t *testing.T) {
	prev := Global
	Global = WorkspaceConfig{WorkspaceRoot: "/repo"}
	t.Cleanup(func() { Global = prev })
	rel, err := GetPathRelativeToWorkspaceRoot("/repo/pkg/sub")
	if err != nil {
		t.Fatal(err)
	}
	if rel != filepath.Join("pkg", "sub") {
		t.Fatalf("got %q", rel)
	}
	if _, err := GetPathRelativeToWorkspaceRoot("/elsewhere/x"); err == nil {
		t.Fatal("want err")
	}
}

func TestGetPathAbsoluteToWorkspaceRoot(t *testing.T) {
	prev := Global
	Global = WorkspaceConfig{WorkspaceRoot: "/repo"}
	t.Cleanup(func() { Global = prev })
	if got := GetPathAbsoluteToWorkspaceRoot("a/b"); got != filepath.Join("/repo", "a", "b") {
		t.Fatalf("got %q", got)
	}
}

func TestGetPackagePath(t *testing.T) {
	prev := Global
	Global = WorkspaceConfig{WorkspaceRoot: "/repo"}
	t.Cleanup(func() { Global = prev })
	pkg, err := GetPackagePath("/repo/pkg/a/file.txt")
	if err != nil {
		t.Fatal(err)
	}
	if pkg != filepath.Join("pkg", "a") {
		t.Fatalf("got %q", pkg)
	}
	if _, err := GetPackagePath("/elsewhere/x"); err == nil {
		t.Fatal("want err")
	}
}

func TestGetWorkspaceCachePrefix_IsPathUnique(t *testing.T) {
	// Logs and the workspace lock rely on this prefix staying unique per
	// absolute path so that parallel grog invocations in different
	// checkouts of the same repo don't clobber each other.
	a := GetWorkspaceCachePrefix("/tmp/builds/abc/myrepo")
	b := GetWorkspaceCachePrefix("/tmp/builds/def/myrepo")
	if a == b {
		t.Fatalf("expected different prefixes for different paths, got %q twice", a)
	}
	if !strings.HasSuffix(a, "-myrepo") {
		t.Fatalf("expected suffix -myrepo, got %q", a)
	}
}

func TestGetWorkspaceCacheDirectory_FlatByDefault(t *testing.T) {
	prev := Global
	Global = WorkspaceConfig{Root: "/grog", WorkspaceRoot: "/tmp/a/myrepo"}
	t.Cleanup(func() { Global = prev })

	got := Global.GetWorkspaceCacheDirectory()
	want := filepath.Join("/grog", "cache")
	if got != want {
		t.Fatalf("expected flat cache dir %q, got %q", want, got)
	}

	// Same cache dir regardless of checkout path — the CI-sharing property.
	Global.WorkspaceRoot = "/ephemeral/runner-42/myrepo"
	if Global.GetWorkspaceCacheDirectory() != want {
		t.Fatalf("expected cache dir to be stable across checkouts")
	}
}

func TestGetWorkspaceCacheDirectory_Namespaced(t *testing.T) {
	prev := Global
	Global = WorkspaceConfig{
		Root:           "/grog",
		WorkspaceRoot:  "/tmp/a/myrepo",
		CacheNamespace: "team-myrepo",
	}
	t.Cleanup(func() { Global = prev })

	got := Global.GetWorkspaceCacheDirectory()
	want := filepath.Join("/grog", "team-myrepo", "cache")
	if got != want {
		t.Fatalf("expected namespaced cache dir %q, got %q", want, got)
	}
}

func TestGetWorkspaceRootDir_StaysPathIsolated(t *testing.T) {
	// The workspace root dir (logs, lockfile) must stay path-unique even
	// when CacheNamespace is set — otherwise parallel checkouts could
	// deadlock on the same lockfile.
	prev := Global
	Global = WorkspaceConfig{
		Root:           "/grog",
		WorkspaceRoot:  "/tmp/a/myrepo",
		CacheNamespace: "team-myrepo",
	}
	t.Cleanup(func() { Global = prev })

	a := Global.GetWorkspaceRootDir()

	Global.WorkspaceRoot = "/tmp/b/myrepo"
	b := Global.GetWorkspaceRootDir()

	if a == b {
		t.Fatalf("expected workspace root dir to differ between checkouts, got %q twice", a)
	}
}
