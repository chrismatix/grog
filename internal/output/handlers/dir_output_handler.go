package handlers

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"fmt"
	"grog/internal/caching"
	"grog/internal/config"
	"grog/internal/console"
	"grog/internal/model"
	v1 "grog/internal/proto/gen"
	"io"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/cespare/xxhash/v2"
)

// DirectoryOutputHandler handles directory outputs by compressing them to tar.gz files
// and using the target cache to store them.
type DirectoryOutputHandler struct {
	cas *caching.Cas
}

// NewDirectoryOutputHandler creates a new DirectoryOutputHandler
func NewDirectoryOutputHandler(targetCache *caching.TargetCache, cas *caching.Cas) *DirectoryOutputHandler {
	return &DirectoryOutputHandler{
		cas: cas,
	}
}

func (d *DirectoryOutputHandler) Type() HandlerType {
	return DirHandler
}

// Has checks if the directory output exists in the cache
func (d *DirectoryOutputHandler) Has(ctx context.Context, output *v1.Output) (bool, error) {
	// We check for the existence of the compressed file in the cache
	return d.cas.Exists(ctx, output.GetDirectory().GetTreeDigest().Hash)
}

// Write compresses a directory into <output>.tar.gz and streams it into the cache.
func (d *DirectoryOutputHandler) Write(
	ctx context.Context,
	target model.Target,
	output model.Output,
) (string, error) {
	logger := console.GetLogger(ctx)

	workspaceRelativePath := filepath.Join(target.Label.Package, output.Identifier)
	directoryPath := config.GetPathAbsoluteToWorkspaceRoot(workspaceRelativePath)

	logger.Debugf("compressing %s (target %s → %s)", directoryPath, target.Label, output)

	return nil
}

// Load extracts the compressed directory from the cache and writes it to the target directory.
//
// This is a bit more complicated than the Write method because we need to extract the compressed
// data to a temporary file first, then read it back into a tar reader and extract the files.
//
// This is because the tar reader doesn't support reading from a stream, so we need to write the
// compressed data to a temporary file first.
func (d *DirectoryOutputHandler) Load(ctx context.Context, target model.Target, output model.Output) (string, error) {
	logger := console.GetLogger(ctx)
	dirPath := config.GetPathAbsoluteToWorkspaceRoot(filepath.Join(target.Label.Package, output.Identifier))

	// Create a temporary file to store the compressed data
	tempFile, err := os.CreateTemp("", fmt.Sprintf("%s_dir_output_*.tar.gz", target.ChangeHash))
	if err != nil {
		return "", err
	}
	tempFilePath := tempFile.Name()
	defer console.WarnOnError(ctx, func() error {
		if err := os.Remove(tempFilePath); err != nil {
			return fmt.Errorf("failed to clean up tmp tar file file %s: %w", tempFilePath, err)
		}
		return nil
	})

	// Get the compressed file from the cache
	contentReader, err := d.targetCache.LoadFileStream(ctx, target, output)
	if err != nil {
		return "", err
	}
	defer console.WarnOnError(ctx, contentReader.Close)

	hasher := xxhash.New()
	hashedReader := io.TeeReader(contentReader, hasher)

	// Copy the compressed data to the temporary file
	if _, err := io.Copy(tempFile, hashedReader); err != nil {
		return "", err
	}

	// Close the file to ensure all data is written
	if err := tempFile.Close(); err != nil {
		return "", err
	}

	// Reopen the temporary file for reading
	tempFile, err = os.Open(tempFilePath)
	if err != nil {
		return "", err
	}
	defer console.WarnOnError(ctx, tempFile.Close)

	// Create the target directory if it doesn't exist
	if err := os.MkdirAll(dirPath, 0755); err != nil {
		return "", err
	}

	// Create a gzip reader
	// NOTE does not need to be closed since it's closed by the tar reader
	gzipReader, err := gzip.NewReader(tempFile)
	if err != nil {
		return "", err
	}

	// Create a tar reader
	tarReader := tar.NewReader(gzipReader)

	// Extract the files
	fileCount := 0
	for {
		header, tarError := tarReader.Next()
		if tarError == io.EOF {
			break
		}
		if tarError != nil {
			return "", fmt.Errorf("error reading tar header: %w", tarError)
		}

		// Get the target path
		targetPath := filepath.Join(dirPath, header.Name)

		switch header.Typeflag {
		case tar.TypeDir:
			// Remove any files that already exist
			if err := os.RemoveAll(targetPath); err != nil {
				return "", err
			}
			if err := os.MkdirAll(targetPath, 0755); err != nil {
				return "", err
			}
			fileCount++
			continue

		case tar.TypeSymlink:
			// Ensure the parent directory exists
			if err := os.MkdirAll(filepath.Dir(targetPath), 0755); err != nil {
				return "", fmt.Errorf("failed to create parent directories for symlink %s: %w", targetPath, err)
			}

			// Remove any existing file/symlink so os.Symlink doesn't fail
			if err := os.RemoveAll(targetPath); err != nil {
				return "", fmt.Errorf("failed to remove existing path for symlink %s: %w", targetPath, err)
			}

			// header.Linkname holds the symlink target as written by FileInfoHeader
			if err := os.Symlink(header.Linkname, targetPath); err != nil {
				return "", fmt.Errorf("failed to create symlink %s -> %s: %w", targetPath, header.Linkname, err)
			}

			fileCount++
			continue

		default:
			// Create parent directories if needed
			if err := os.MkdirAll(filepath.Dir(targetPath), 0755); err != nil {
				return "", fmt.Errorf("failed to create parent directories for file %s: %w", targetPath, err)
			}

			// Create the file
			file, tarError := os.OpenFile(targetPath, os.O_CREATE|os.O_WRONLY, os.FileMode(header.Mode))
			if tarError != nil {
				return "", fmt.Errorf("failed to create file %s: %w", targetPath, tarError)
			}

			if _, err := io.Copy(file, tarReader); err != nil {
				return "", fmt.Errorf("failed to copy file %s: %w", targetPath, err)
			}

			if err := file.Close(); err != nil {
				return "", fmt.Errorf("failed to close file %s: %w", targetPath, err)
			}

			fileCount++
		}
	}

	logger.Debugf("Successfully extracted %d files to %s", fileCount, dirPath)
	return fmt.Sprintf("%x", hasher.Sum64()), nil
}
