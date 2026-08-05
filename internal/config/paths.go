package config

import (
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/fatih/color"
)

// GetWorkspaceCachePrefix returns the per-checkout prefix used for workspace
// ephemera (logs, workspace lock) and for remote cache paths when
// shared_cache is disabled. It combines a SHA-256 hash of the absolute
// workspace path with the directory basename so that concurrent grog
// invocations in different checkouts of the same repo cannot clobber each
// other's logs or deadlock on the same lock file. The target cache itself is
// not prefixed with this value — it lives directly under $GROG_ROOT and is
// shared across checkouts (see WorkspaceConfig.GetWorkspaceCacheDirectory).
func GetWorkspaceCachePrefix(workspaceDir string) string {
	repoHash := fmt.Sprintf("%x", sha256.Sum256([]byte(workspaceDir)))[:16]
	workspaceName := filepath.Base(workspaceDir)
	return fmt.Sprintf("%s-%s", repoHash, workspaceName)
}

// FindWorkspaceRoot searches for a grog.toml in the current directory and its parents.
func FindWorkspaceRoot() (string, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("failed to get current working directory: %w", err)
	}

	for {
		configPath := filepath.Join(cwd, "grog.toml")
		if _, err := os.Stat(configPath); err == nil {
			return cwd, nil
		} else if !os.IsNotExist(err) {
			return "", fmt.Errorf("failed to check for grog.toml: %w", err)
		}

		parent := filepath.Dir(cwd)
		if parent == cwd {
			break
		}
		cwd = parent
	}

	return "", fmt.Errorf("grog.toml not found in any parent directory. Is this a grog workspace?")
}

// MustFindWorkspaceRoot returns the workspace root or exits.
func MustFindWorkspaceRoot() string {
	workspaceRoot, err := FindWorkspaceRoot()
	if err != nil {
		fmt.Printf("%s %v\n", color.RedString("FATAL:"), err)
		os.Exit(1)
	}
	return workspaceRoot
}

func GetPathRelativeToWorkspaceRoot(path string) (string, error) {
	workspaceRoot := Global.WorkspaceRoot
	// error if path is not under workspace root
	if !strings.HasPrefix(path, workspaceRoot) {
		return "", fmt.Errorf("path %s is not under workspace root %s", path, workspaceRoot)
	}

	return path[len(workspaceRoot)+1:], nil
}

func GetPathAbsoluteToWorkspaceRoot(path string) string {
	workspaceRoot := Global.WorkspaceRoot
	return filepath.Join(workspaceRoot, path)
}

func GetPackagePath(path string) (string, error) {
	relativePath, err := GetPathRelativeToWorkspaceRoot(path)
	if err != nil {
		return "", err
	}
	// get dir and remove the last slash
	dirPath := filepath.Dir(relativePath)
	return strings.TrimSuffix(dirPath, "/"), nil
}
