package config

import "testing"

func TestGetGitHashAndBranch(t *testing.T) {
	hash, err := GetGitHash()
	if err != nil {
		t.Fatalf("GetGitHash: %v", err)
	}
	if len(hash) < 7 {
		t.Fatalf("hash too short: %q", hash)
	}

	branch, err := GetGitBranch()
	if err != nil {
		t.Fatalf("GetGitBranch: %v", err)
	}
	if branch == "" {
		t.Fatal("expected non-empty branch")
	}
}
