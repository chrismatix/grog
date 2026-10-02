package loading

import (
	"io/fs"
	"path"
	"slices"
	"strings"

	"github.com/bmatcuk/doublestar/v4"
)

// excludingFS hides directories that an exclude pattern ending in "/**" covers
// entirely, so globbing never descends into them.
type excludingFS struct {
	fs.FS

	excludeInputs []string
}

func (filesystem excludingFS) ReadDir(name string) ([]fs.DirEntry, error) {
	entries, err := fs.ReadDir(filesystem.FS, name)
	if err != nil {
		return nil, err
	}
	keptEntries := entries[:0]
	for _, entry := range entries {
		directoryPath := path.Join(name, entry.Name())
		if entry.IsDir() && slices.ContainsFunc(filesystem.excludeInputs, func(pattern string) bool {
			return strings.HasSuffix(pattern, "/**") && doublestar.MatchUnvalidated(pattern, directoryPath)
		}) {
			continue
		}
		keptEntries = append(keptEntries, entry)
	}
	return keptEntries, nil
}
