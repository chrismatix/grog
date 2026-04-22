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

// MustFindWorkspaceRoot searches for the repository root by looking for "grog.toml"
// in the current working directory and its parents. It panics if it
// does not find the file.
func MustFindWorkspaceRoot() string {
	cwd, err := os.Getwd()
	if err != nil {
		fmt.Printf("%s failed to get current working directory: %v\n", color.RedString("FATAL:"), err)
		os.Exit(1)
	}

	for {
		configPath := filepath.Join(cwd, "grog.toml")
		if _, err := os.Stat(configPath); err == nil {
			return cwd
		} else if !os.IsNotExist(err) {
			fmt.Printf("%s failed to check for grog.toml: %v\n", color.RedString("FATAL:"), err)
			os.Exit(1)
		}

		parent := filepath.Dir(cwd)
		if parent == cwd {
			break
		}
		cwd = parent
	}

	fmt.Printf("%s grog.toml not found in any parent directory. Is this a grog workspace?\n", color.RedString("FATAL:"))
	os.Exit(1)
	return "" // unreachable but needed to satisfy compiler
}

func GetPathRelativeToWorkspaceRoot(path string) (string, error) {
	workspaceRoot, err := filepath.Abs(Global.WorkspaceRoot)
	if err != nil {
		return "", err
	}

	absolutePath, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}

	relativePath, err := filepath.Rel(workspaceRoot, absolutePath)
	if err != nil {
		return "", err
	}
	if relativePath == "." {
		return "", nil
	}
	if strings.HasPrefix(relativePath, ".."+string(filepath.Separator)) || relativePath == ".." || filepath.IsAbs(relativePath) {
		return "", fmt.Errorf("path %s is not under workspace root %s", path, Global.WorkspaceRoot)
	}

	return filepath.ToSlash(relativePath), nil
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
	dirPath := filepath.ToSlash(filepath.Dir(relativePath))
	return strings.TrimSuffix(dirPath, "/"), nil
}
