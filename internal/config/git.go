package config

import (
	"bytes"
	"os/exec"
	"strings"
)

// GetGitHash returns the current git hash.
func GetGitHash() (string, error) {
	cmd := exec.Command("git", "rev-parse", "HEAD")
	var out bytes.Buffer
	cmd.Stdout = &out

	if err := cmd.Run(); err != nil {
		return "", err
	}
	return strings.TrimSpace(out.String()), nil
}

// GetGitBranch returns the current git branch name.
func GetGitBranch() (string, error) {
	cmd := exec.Command("git", "rev-parse", "--abbrev-ref", "HEAD")
	var out bytes.Buffer
	cmd.Stdout = &out

	if err := cmd.Run(); err != nil {
		return "", err
	}
	return strings.TrimSpace(out.String()), nil
}

// GetGitRoot returns the absolute path of the repository's top-level directory,
// or an error if the working directory is not inside a git repository.
func GetGitRoot() (string, error) {
	cmd := exec.Command("git", "rev-parse", "--show-toplevel")
	var out bytes.Buffer
	cmd.Stdout = &out
	if err := cmd.Run(); err != nil {
		return "", err
	}
	return strings.TrimSpace(out.String()), nil
}

// ResolveCacheSeedRef returns the git ref to use for Docker layer-cache
// seed-donor selection, resolving the empty (auto-detect) case.
//
// When explicitRef is non-empty it is returned verbatim (callers pass the same
// ref they use for `grog changes`: origin/<base-branch> on pull requests, HEAD~1
// on push-to-default). When empty, the merge-base with the upstream default
// branch is tried first (the natural predecessor for a feature branch), falling
// back to HEAD~1. The returned boolean is false when no usable ref could be
// resolved (e.g. a shallow clone with no parent), in which case seeding is
// skipped entirely.
func ResolveCacheSeedRef(explicitRef string) (string, bool) {
	if explicitRef != "" {
		return explicitRef, true
	}

	// Prefer the merge-base with the upstream default branch (origin/HEAD).
	if base := gitMergeBaseWithUpstreamDefault(); base != "" {
		return base, true
	}

	// Fall back to the immediate parent of HEAD.
	if revExists("HEAD~1") {
		return "HEAD~1", true
	}

	return "", false
}

// gitMergeBaseWithUpstreamDefault returns the merge-base commit between HEAD and
// the remote's default branch (origin/HEAD), or "" if it cannot be determined.
func gitMergeBaseWithUpstreamDefault() string {
	// origin/HEAD points at the remote's default branch (e.g. origin/main).
	if !revExists("origin/HEAD") {
		return ""
	}
	cmd := exec.Command("git", "merge-base", "HEAD", "origin/HEAD")
	var out bytes.Buffer
	cmd.Stdout = &out
	if err := cmd.Run(); err != nil {
		return ""
	}
	return strings.TrimSpace(out.String())
}

// revExists reports whether rev resolves to a commit in the current repository.
func revExists(rev string) bool {
	cmd := exec.Command("git", "rev-parse", "--verify", "--quiet", rev+"^{commit}")
	return cmd.Run() == nil
}
