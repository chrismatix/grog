package hashing

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"

	"github.com/cespare/xxhash/v2"
)

// HashDirectory computes a deterministic hash for all files contained in rootPath.
// The hash includes file paths, file contents, file modes, and symlink targets
// to make sure that structural changes are detected as well.
func HashDirectory(rootPath string) (string, error) {
	hasher := xxhash.New()

	var relPaths []string
	err := filepath.WalkDir(rootPath, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if path == rootPath {
			return nil
		}
		rel, err := filepath.Rel(rootPath, path)
		if err != nil {
			return err
		}
		relPaths = append(relPaths, filepath.ToSlash(rel))
		return nil
	})
	if err != nil {
		return "", err
	}

	sort.Strings(relPaths)

	for _, rel := range relPaths {
		fullPath := filepath.Join(rootPath, rel)
		info, err := os.Lstat(fullPath)
		if err != nil {
			return "", err
		}

		mode := info.Mode()
		switch {
		case mode.IsDir():
			if _, err := hasher.WriteString(fmt.Sprintf("dir:%s:%o;", rel, mode.Perm())); err != nil {
				return "", err
			}
		case mode&os.ModeSymlink != 0:
			target, err := os.Readlink(fullPath)
			if err != nil {
				return "", err
			}
			if _, err := hasher.WriteString(fmt.Sprintf("symlink:%s:%s;", rel, target)); err != nil {
				return "", err
			}
		default:
			fileHash, err := HashFile(fullPath)
			if err != nil {
				return "", err
			}
			if _, err := hasher.WriteString(fmt.Sprintf("file:%s:%s;", rel, fileHash)); err != nil {
				return "", err
			}
		}
	}

	return fmt.Sprintf("%x", hasher.Sum64()), nil
}
