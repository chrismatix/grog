package backends

import (
	"context"
	"errors"
	"fmt"
	"grog/internal/config"
	"grog/internal/console"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// fsStagingDirName is the subdirectory under the shared CAS directory where
// in-flight uploads are buffered. Keeping it inside the CAS directory means
// the final commit can be a same-filesystem os.Rename rather than a
// cross-device copy.
const fsStagingDirName = ".staging"

// FileSystemCache implements the CacheBackend interface using the file system for storage
type FileSystemCache struct {
	workspaceCacheDir string
	sharedCasDir      string
}

func (fsc *FileSystemCache) TypeName() string {
	return "fs"
}

// NewFileSystemCache creates a new cache using the configured cache directory
func NewFileSystemCache(ctx context.Context) (*FileSystemCache, error) {
	workspaceCacheDir := config.Global.GetWorkspaceCacheDirectory()
	sharedCasDir := config.Global.GetCasDirectory()

	// Ensure the root directory exists
	if err := os.MkdirAll(workspaceCacheDir, 0755); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(sharedCasDir, 0755); err != nil {
		return nil, err
	}

	console.GetLogger(ctx).Tracef("Instantiated fs cache at: %s", workspaceCacheDir)
	return &FileSystemCache{
		workspaceCacheDir: workspaceCacheDir,
		sharedCasDir:      sharedCasDir,
	}, nil
}

// buildFilePath constructs the full file path for a cached item
func (fsc *FileSystemCache) buildFilePath(path, key string) string {
	dir := fsc.getDir(path)
	return filepath.Join(dir, key)
}

func (fsc *FileSystemCache) getDir(path string) string {
	if path == "cas" {
		return fsc.sharedCasDir
	}
	return filepath.Join(fsc.workspaceCacheDir, path)
}

// Get retrieves a cached file by its key
func (fsc *FileSystemCache) Get(ctx context.Context, path, key string) (io.ReadCloser, error) {
	logger := console.GetLogger(ctx)
	logger.Tracef("Getting file from cache for path: %s, key: %s", path, key)

	filePath := fsc.buildFilePath(path, key)

	file, err := os.Open(filePath)
	if err != nil {
		logger.Tracef("Failed to get file for path: %s, key: %s", path, key)
		return nil, err
	}

	return file, err
}

// Set stores a file in the cache with the given key and content
func (fsc *FileSystemCache) Set(ctx context.Context, path, key string, content io.Reader) error {
	logger := console.GetLogger(ctx)
	logger.Tracef("Setting file in cache for path: %s, key: %s", path, key)

	filePath := fsc.buildFilePath(path, key)
	destinationDirectory := filepath.Dir(filePath)

	// Make sure the destination directory exists. Keys can include path separators.
	if err := os.MkdirAll(destinationDirectory, 0755); err != nil {
		return err
	}

	// Write to a temp file first to ensure atomicity
	tmpFile, err := os.CreateTemp(destinationDirectory, "tmp-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmpFile.Name()) // Cleanup temp file if rename fails

	// Copy the content from the reader to the file
	if _, err = io.Copy(tmpFile, content); err != nil {
		tmpFile.Close()
		return err
	}

	// Close explicitly before rename
	if err := tmpFile.Close(); err != nil {
		return err
	}

	return os.Rename(tmpFile.Name(), filePath)
}

// Delete removes a cached file by its key
func (fsc *FileSystemCache) Delete(ctx context.Context, path, key string) error {
	logger := console.GetLogger(ctx)
	logger.Tracef("Deleting file from cache for path: %s, key: %s", path, key)

	filePath := fsc.buildFilePath(path, key)

	// Check if file exists before attempting to remove
	if _, err := os.Stat(filePath); os.IsNotExist(err) {
		logger.Tracef("File not found for deletion for path: %s, key: %s", path, key)
		return nil
	}

	return os.Remove(filePath)
}

// Exists checks if a file exists in the cache with the given key
func (fsc *FileSystemCache) Exists(ctx context.Context, path, key string) (bool, error) {
	logger := console.GetLogger(ctx)

	filePath := fsc.buildFilePath(path, key)

	_, err := os.Stat(filePath)
	if err != nil {
		if os.IsNotExist(err) {
			logger.Tracef("Cache-miss for path: %s, key: %s", path, key)
			return false, nil
		}
		logger.Tracef("Cache failed for path: %s, key: %s %v", path, key, err)
		return false, err
	}
	logger.Tracef("Cache-hit for path: %s, key: %s", path, key)
	return true, nil
}

// Size returns the size in bytes of the cached file with the given key, or
// an error if the file is missing. Backends that don't implement SizeAware
// fall back to reading the entry; this implementation is just an os.Stat.
func (fsc *FileSystemCache) Size(_ context.Context, path, key string) (int64, error) {
	filePath := fsc.buildFilePath(path, key)
	info, err := os.Stat(filePath)
	if err != nil {
		return 0, err
	}
	return info.Size(), nil
}

// BeginWrite opens a temp file in a staging directory inside the shared CAS
// directory. Because the staging file lives on the same filesystem as the
// final destination, Commit can be a single os.Rename — no double copy.
func (fsc *FileSystemCache) BeginWrite(ctx context.Context) (StagedWriter, error) {
	// We always stage inside the shared CAS directory so the final move is a
	// same-filesystem rename. Workspace-cache writes use a sibling subdir to
	// avoid mixing temp data with the namespaced workspace tree.
	stagingDir := filepath.Join(fsc.sharedCasDir, fsStagingDirName)
	if err := os.MkdirAll(stagingDir, 0755); err != nil {
		return nil, fmt.Errorf("create staging dir: %w", err)
	}

	f, err := os.CreateTemp(stagingDir, "upload-*")
	if err != nil {
		return nil, fmt.Errorf("create staging file: %w", err)
	}

	console.GetLogger(ctx).Tracef("fs cache opened staging file %s", f.Name())
	return &fsStagedWriter{file: f, fsc: fsc}, nil
}

// fsStagedWriter is the FileSystemCache StagedWriter. It writes incoming
// bytes directly to a same-filesystem temp file and promotes the file to its
// final CAS location at Commit time via a single rename.
type fsStagedWriter struct {
	fsc *FileSystemCache

	mu   sync.Mutex
	file *os.File // nil after Commit/Cancel
}

func (w *fsStagedWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.file == nil {
		return 0, errors.New("fs staged writer: write after commit/cancel")
	}
	return w.file.Write(p)
}

func (w *fsStagedWriter) Commit(_ context.Context, path, key string) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.file == nil {
		return errors.New("fs staged writer: commit after commit/cancel")
	}

	stagedPath := w.file.Name()
	// Close before rename so the file's contents are flushed to the OS.
	if err := w.file.Close(); err != nil {
		_ = os.Remove(stagedPath)
		w.file = nil
		return fmt.Errorf("close staging file: %w", err)
	}
	w.file = nil

	finalPath := w.fsc.buildFilePath(path, key)
	finalDir := filepath.Dir(finalPath)
	if err := os.MkdirAll(finalDir, 0755); err != nil {
		_ = os.Remove(stagedPath)
		return fmt.Errorf("create destination dir: %w", err)
	}

	if err := os.Rename(stagedPath, finalPath); err != nil {
		_ = os.Remove(stagedPath)
		return fmt.Errorf("rename staging file: %w", err)
	}
	return nil
}

func (w *fsStagedWriter) Cancel(_ context.Context) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.file == nil {
		// Already committed or cancelled — Cancel is idempotent.
		return nil
	}
	stagedPath := w.file.Name()
	_ = w.file.Close()
	w.file = nil
	if err := os.Remove(stagedPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove staging file: %w", err)
	}
	return nil
}

// ListKeys walks the directory tree and returns keys matching the suffix.
// In-flight uploads under the .staging subdirectory are skipped so they
// never leak into cache enumerations.
func (fsc *FileSystemCache) ListKeys(ctx context.Context, path string, suffix string) ([]string, error) {
	dir := fsc.getDir(path)

	var keys []string
	err := filepath.Walk(dir, func(filePath string, info os.FileInfo, err error) error {
		if err != nil {
			if os.IsNotExist(err) {
				return nil
			}
			return err
		}
		if info.IsDir() {
			if info.Name() == fsStagingDirName && filePath != dir {
				return filepath.SkipDir
			}
			return nil
		}
		rel, relErr := filepath.Rel(dir, filePath)
		if relErr != nil {
			return relErr
		}
		if suffix == "" || strings.HasSuffix(rel, suffix) {
			keys = append(keys, rel)
		}
		return nil
	})

	if err != nil && os.IsNotExist(err) {
		return nil, nil
	}
	return keys, err
}

func (fsc *FileSystemCache) GetWorkspaceCacheSizeBytes() (int64, error) {
	return getDirectorySizeBytes(fsc.workspaceCacheDir)
}

func (fsc *FileSystemCache) GetCacheSizeBytes() (int64, error) {
	return getDirectorySizeBytes(config.Global.Root)
}

func getDirectorySizeBytes(path string) (int64, error) {
	var size int64
	err := filepath.Walk(path, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !info.IsDir() {
			size += info.Size()
		}
		return nil
	})

	return size, err
}
