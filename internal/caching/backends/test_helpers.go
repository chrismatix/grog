package backends

// NewFileSystemCacheForTest creates a FileSystemCache with custom directories.
// Intended for use in tests outside this package.
func NewFileSystemCacheForTest(workspaceCacheDir, sharedCasDir string) *FileSystemCache {
	return &FileSystemCache{
		workspaceCacheDir: workspaceCacheDir,
		sharedCasDir:      sharedCasDir,
	}
}
