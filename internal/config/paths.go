package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/fatih/color"
)

// GetWorkspaceCachePrefix returns the name of the cache directory for the
// current repo. By default the prefix is derived from the workspace directory's
// basename, which keeps the cache namespace stable across different absolute
// paths of the same repo (e.g. ephemeral CI runners).
// Setting the workspace_name option overrides this to isolate workspaces that
// would otherwise share a basename (for example two forks both checked out
// under a directory called "myrepo").
func GetWorkspaceCachePrefix(workspaceDir string) string {
	if Global.WorkspaceName != "" {
		return Global.WorkspaceName
	}
	return filepath.Base(workspaceDir)
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
