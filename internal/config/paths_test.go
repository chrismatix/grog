package config

import "testing"

func TestGetWorkspaceCachePrefix_DefaultsToBasename(t *testing.T) {
	prev := Global.WorkspaceName
	Global.WorkspaceName = ""
	t.Cleanup(func() { Global.WorkspaceName = prev })

	if got := GetWorkspaceCachePrefix("/tmp/builds/abc/myrepo"); got != "myrepo" {
		t.Fatalf("expected basename, got %q", got)
	}

	// Same repo checked out at a different absolute path must resolve to the
	// same prefix — the property the default is designed to provide.
	if got := GetWorkspaceCachePrefix("/ephemeral/ci/runner-42/myrepo"); got != "myrepo" {
		t.Fatalf("expected stable prefix across checkouts, got %q", got)
	}
}

func TestGetWorkspaceCachePrefix_Override(t *testing.T) {
	prev := Global.WorkspaceName
	Global.WorkspaceName = "org-myrepo"
	t.Cleanup(func() { Global.WorkspaceName = prev })

	if got := GetWorkspaceCachePrefix("/tmp/builds/abc/myrepo"); got != "org-myrepo" {
		t.Fatalf("expected override to take precedence, got %q", got)
	}
}
