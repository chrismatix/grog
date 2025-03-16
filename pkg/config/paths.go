package config

import (
	"crypto/sha256"
	"fmt"
	"github.com/spf13/viper"
	"os"
	"path/filepath"
	"strings"
)

// GetWorkspaceCacheDirectory returns the path to the cache directory for the current repo.
// Like Bazel we hash the repo path and use that as directory within $GROG_CACHE_DIR
func GetWorkspaceCacheDirectory(repoUrl string) string {
	cacheDir := viper.GetString("cache_dir")

	repoHash := fmt.Sprintf("%x", sha256.Sum256([]byte(repoUrl)))

	return fmt.Sprintf("%s/%s", cacheDir, repoHash)
}

// MustFindWorkspaceRoot searches for the repository root by looking for "grog.toml"
// in the current working directory and its parents. It panics if it
// does not find the file.
func MustFindWorkspaceRoot() string {
	cwd, err := os.Getwd()
	if err != nil {
		panic(fmt.Sprintf("failed to get current working directory: %v", err))
	}

	for {
		configPath := filepath.Join(cwd, "grog.toml")
		if _, err := os.Stat(configPath); err == nil {
			return cwd
		} else if !os.IsNotExist(err) {
			panic(fmt.Sprintf("failed to check for grog.toml: %v", err))
		}

		parent := filepath.Dir(cwd)
		if parent == cwd {
			break
		}
		cwd = parent
	}

	panic("grog.toml not found in any parent directory. Is this a grog workspace?")
}

func GetPathRelativeToWorkspaceRoot(path string) (string, error) {
	workspaceRoot := viper.Get("workspace_root").(string)
	// error if path is not under workspace root
	if !strings.HasPrefix(path, workspaceRoot) {
		return "", fmt.Errorf("path %s is not under workspace root %s", path, workspaceRoot)
	}

	return path[len(workspaceRoot)+1:], nil
}
