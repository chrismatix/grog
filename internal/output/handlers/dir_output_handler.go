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
	"io"
	"io/fs"
	"os"
	"path/filepath"
)

// DirectoryOutputHandler handles directory outputs by compressing them to tar.gz files
// and using the target cache to store them.
type DirectoryOutputHandler struct {
	targetCache *caching.TargetCache
}

// NewDirectoryOutputHandler creates a new DirectoryOutputHandler
func NewDirectoryOutputHandler(targetCache *caching.TargetCache) *DirectoryOutputHandler {
	return &DirectoryOutputHandler{
		targetCache: targetCache,
	}
}

func (d *DirectoryOutputHandler) Type() HandlerType {
	return DirHandler
}

// Has checks if the directory output exists in the cache
func (d *DirectoryOutputHandler) Has(ctx context.Context, target model.Target, output model.Output) (bool, error) {
	// We check for the existence of the compressed file in the cache
	return d.targetCache.FileExists(ctx, target, output)
}

// Write compresses a directory into <output>.tar.gz and streams it into the cache.
func (d *DirectoryOutputHandler) Write(
	ctx context.Context,
	target model.Target,
	output model.Output,
) error {
	logger := console.GetLogger(ctx)

	workspaceRelativePath := filepath.Join(target.Label.Package, output.Identifier)
	directoryPath := config.GetPathAbsoluteToWorkspaceRoot(workspaceRelativePath)

	logger.Debugf("compressing %s (target %s → %s)", directoryPath, target.Label, output)

	pipeReader, pipeWriter := io.Pipe()
	errChan := make(chan error, 1)

	go func() {
		gzipWriter := gzip.NewWriter(pipeWriter)
		tarWriter := tar.NewWriter(gzipWriter)

		walkErr := filepath.WalkDir(directoryPath, func(path string, directoryEntry fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if path == directoryPath { // skip the root directory entry
				return nil
			}

			fileInfo, err := directoryEntry.Info()
			if err != nil {
				return err
			}

			// Handle symlinks differently:
			if fileInfo.Mode()&os.ModeSymlink != 0 {
				linkTarget, err := os.Readlink(path)
				if err != nil {
					return err
				}
				// Just link to the target in the header
				header, err := tar.FileInfoHeader(fileInfo, linkTarget)
				if err != nil {
					return err
				}
				relativePath, err := filepath.Rel(directoryPath, path)
				if err != nil {
					return err
				}
				header.Name = filepath.ToSlash(relativePath)
				header.Format = tar.FormatPAX

				if err := tarWriter.WriteHeader(header); err != nil {
					return err
				}
				// Do not copy file content for symlink.
				return nil
			}

			header, err := tar.FileInfoHeader(fileInfo, "")
			if err != nil {
				return err
			}

			relativePath, err := filepath.Rel(directoryPath, path)
			if err != nil {
				return err
			}
			header.Name = filepath.ToSlash(relativePath) // POSIX‑style paths in the archive
			header.Format = tar.FormatPAX

			if err := tarWriter.WriteHeader(header); err != nil {
				return err
			}

			if !directoryEntry.IsDir() {
				file, err := os.Open(path)
				if err != nil {
					return err
				}
				if _, err := io.Copy(tarWriter, file); err != nil {
					logger.Debugf("Error writing file %s error: %v", path, err)
					file.Close()
					return err
				}
				file.Close()
			}
			return nil
		})

		// Ensure tar and gzip writers are closed and their errors captured
		if err := tarWriter.Close(); err != nil {
			pipeWriter.CloseWithError(err)
			errChan <- err
			return
		}
		if err := gzipWriter.Close(); err != nil {
			pipeWriter.CloseWithError(err)
			errChan <- err
			return
		}

		// Propagate any walk error after writers are closed
		if walkErr != nil {
			pipeWriter.CloseWithError(walkErr)
			errChan <- walkErr
		} else {
			pipeWriter.Close()
			errChan <- nil
		}
	}()

	logger.Debug("streaming tar.gz to cache")
	writeErr := d.targetCache.WriteFileStream(ctx, target, output, pipeReader)
	// Close the reader in case WriteFileStream returns early to unblock writer goroutine
	pipeReader.Close()
	// Wait for the writing goroutine to finish and capture its error
	goroutineErr := <-errChan
	if writeErr != nil {
		return writeErr
	}
	return goroutineErr
}

// Load extracts the compressed directory from the cache and writes it to the target directory.
//
// This is a bit more complicated than the Write method because we need to extract the compressed
// data to a temporary file first, then read it back into a tar reader and extract the files.
//
// This is because the tar reader doesn't support reading from a stream, so we need to write the
// compressed data to a temporary file first.
func (d *DirectoryOutputHandler) Load(ctx context.Context, target model.Target, output model.Output) error {
	logger := console.GetLogger(ctx)
	dirPath := config.GetPathAbsoluteToWorkspaceRoot(filepath.Join(target.Label.Package, output.Identifier))

	// Create a temporary file to store the compressed data
	tempFile, err := os.CreateTemp("", fmt.Sprintf("%s_dir_output_*.tar.gz", target.ChangeHash))
	if err != nil {
		return err
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
		return err
	}
	defer console.WarnOnError(ctx, contentReader.Close)

	// Copy the compressed data to the temporary file
	if _, err := io.Copy(tempFile, contentReader); err != nil {
		return err
	}

	// Close the file to ensure all data is written
	if err := tempFile.Close(); err != nil {
		return err
	}

	// Reopen the temporary file for reading
	tempFile, err = os.Open(tempFilePath)
	if err != nil {
		return err
	}
	defer console.WarnOnError(ctx, tempFile.Close)

	// Create the target directory if it doesn't exist
	if err := os.MkdirAll(dirPath, 0755); err != nil {
		return err
	}

	// Create a gzip reader
	// NOTE does not need to be closed since it's closed by the tar reader
	gzipReader, err := gzip.NewReader(tempFile)
	if err != nil {
		return err
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
			return fmt.Errorf("error reading tar header: %w", tarError)
		}

		// Get the target path
		targetPath := filepath.Join(dirPath, header.Name)

		// Create directories if needed
		if header.Typeflag == tar.TypeDir {
			if err := os.MkdirAll(targetPath, 0755); err != nil {
				return err
			}
			fileCount++
			continue
		}

		// Create parent directories if needed
		if err := os.MkdirAll(filepath.Dir(targetPath), 0755); err != nil {
			return fmt.Errorf("failed to create parent directories for file %s: %w", targetPath, err)
		}

		// Create the file
		file, tarError := os.OpenFile(targetPath, os.O_CREATE|os.O_WRONLY, os.FileMode(header.Mode))
		if tarError != nil {
			return fmt.Errorf("failed to create file %s: %w", targetPath, tarError)
		}

		if _, err := io.Copy(file, tarReader); err != nil {
			return fmt.Errorf("failed to copy file %s: %w", targetPath, err)
		}

		if err := file.Close(); err != nil {
			return fmt.Errorf("failed to close file %s: %w", targetPath, err)
		}

		fileCount++
	}

	logger.Debugf("Successfully extracted %d files to %s", fileCount, dirPath)
	return nil
}
