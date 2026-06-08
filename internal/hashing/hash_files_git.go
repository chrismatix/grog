package hashing

import (
	"bytes"
	"fmt"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

// HashFilesAtRef computes the combined hash of fileList as the files existed at
// the given git ref, mirroring HashFiles byte-for-byte for identical content.
//
// Paths are interpreted relative to absolutePackagePath (the same contract as
// HashFiles). Each file is read from git via `git cat-file` at gitRef rather
// than from the working tree, so the result reproduces the ChangeHash a prior
// build would have computed without checking the ref out. Files absent at the
// ref are skipped, matching HashFiles' treatment of missing working-tree files,
// so the two functions agree whenever the on-disk and at-ref contents agree.
//
// gitRoot is the absolute path of the repository root; it is needed to turn the
// absolute file paths into repo-relative paths for git's pathspec.
func HashFilesAtRef(gitRoot, gitRef, absolutePackagePath string, fileList []string) (string, error) {
	combinedHasher := GetHasher()
	// Match HashFiles' ordering so the combined hash is order-stable.
	sorted := append([]string(nil), fileList...)
	sort.Strings(sorted)

	for _, file := range sorted {
		absolutePath := filepath.Join(absolutePackagePath, file)
		repoRelativePath, err := filepath.Rel(gitRoot, absolutePath)
		if err != nil {
			return "", fmt.Errorf("failed to relativize %q against git root %q: %w", absolutePath, gitRoot, err)
		}
		// git uses forward slashes in pathspecs regardless of platform.
		repoRelativePath = filepath.ToSlash(repoRelativePath)

		content, exists, err := gitFileContents(gitRoot, gitRef, repoRelativePath)
		if err != nil {
			return "", err
		}
		if !exists {
			// Mirror HashFiles: a file missing at the ref is skipped.
			continue
		}
		if _, err := combinedHasher.Write(content); err != nil {
			return "", err
		}
	}

	return combinedHasher.SumString(), nil
}

// gitFileContents returns the bytes of repoRelativePath at gitRef. The boolean
// is false (with a nil error) when the path does not exist at that ref, which
// callers treat as a skipped file rather than a failure.
func gitFileContents(gitRoot, gitRef, repoRelativePath string) ([]byte, bool, error) {
	// `git show <ref>:<path>` streams the blob at that path and revision.
	cmd := exec.Command("git", "show", fmt.Sprintf("%s:%s", gitRef, repoRelativePath))
	cmd.Dir = gitRoot
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		// git exits non-zero when the path or ref is absent. Distinguish a
		// genuinely-missing path (skip) from an unexpected git failure.
		message := stderr.String()
		if strings.Contains(message, "does not exist") ||
			strings.Contains(message, "exists on disk, but not in") ||
			strings.Contains(message, "unknown revision") ||
			strings.Contains(message, "fatal: invalid object name") ||
			strings.Contains(message, "fatal: path") {
			return nil, false, nil
		}
		return nil, false, fmt.Errorf("git show %s:%s failed: %w: %s", gitRef, repoRelativePath, err, strings.TrimSpace(message))
	}
	return stdout.Bytes(), true, nil
}
