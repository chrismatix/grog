package config

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestWorkspaceRelativePaths(t *testing.T) {
	previous := Global
	workspace := filepath.Join(t.TempDir(), "workspace")
	Global.WorkspaceRoot = workspace
	t.Cleanup(func() { Global = previous })
	for _, testCase := range []struct {
		name      string
		path      string
		want      string
		wantError bool
	}{
		{name: "root", path: workspace, want: "."},
		{name: "nested", path: filepath.Join(workspace, "some", "package", "BUILD.yaml"), want: "some/package/BUILD.yaml"},
		{name: "sibling prefix", path: workspace + "-other/BUILD.yaml", wantError: true},
		{name: "parent", path: filepath.Dir(workspace), wantError: true},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			actual, err := GetPathRelativeToWorkspaceRoot(testCase.path)
			if (err != nil) != testCase.wantError || actual != testCase.want {
				t.Fatalf("got %q, %v; want %q, error=%v", actual, err, testCase.want, testCase.wantError)
			}
		})
	}
	packagePath, err := GetPackagePath(filepath.Join(workspace, "some", "package", "BUILD.yaml"))
	if err != nil || packagePath != "some/package" {
		t.Fatalf("got package %q, %v", packagePath, err)
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
