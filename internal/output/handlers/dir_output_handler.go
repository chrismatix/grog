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

// Write compresses the directory and writes it to the cache as a single file
func (d *DirectoryOutputHandler) Write(ctx context.Context, target model.Target, output model.Output) error {
	dirPath := config.GetPathAbsoluteToWorkspaceRoot(filepath.Join(target.Label.Package, output.Identifier))

	// Create a pipe to stream the compressed data
	pr, pw := io.Pipe()

	// Start a goroutine to compress the directory
	go func() {
		defer console.WarnDeferredError(ctx, pw.Close)

		gw := gzip.NewWriter(pw)
		defer console.WarnDeferredError(ctx, gw.Close)

		tw := tar.NewWriter(gw)
		defer console.WarnDeferredError(ctx, tw.Close)

		// Walk the directory and add files to the tar archive
		// TODO use fastwalk here aswell
		err := filepath.Walk(dirPath, func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return err
			}

			// Skip the root directory itself
			if path == dirPath {
				return nil
			}

			// Create a tar header
			header, err := tar.FileInfoHeader(info, "")
			if err != nil {
				return err
			}

			// Set the name to be relative to the directory being archived
			relPath, err := filepath.Rel(dirPath, path)
			if err != nil {
				return err
			}
			header.Name = relPath

			// Write the header
			if err := tw.WriteHeader(header); err != nil {
				return err
			}

			// If it's a regular file, write the content
			if !info.IsDir() {
				file, err := os.Open(path)
				if err != nil {
					return err
				}
				defer console.WarnDeferredError(ctx, file.Close)

				if _, err := io.Copy(tw, file); err != nil {
					return err
				}
			}

			return nil
		})

		if err != nil {
			pw.CloseWithError(err)
		}
	}()

	// Write the compressed data to the cache
	return d.targetCache.WriteFileStream(ctx, target, output, pr)
}

// Load extracts the compressed directory from the cache and writes it to the target directory.
//
// This is a bit more complicated than the Write method because we need to extract the compressed
// data to a temporary file first, then read it back into a tar reader and extract the files.
//
// This is because the tar reader doesn't support reading from a stream, so we need to write the
// compressed data to a temporary file first.
//
// The temporary file is then opened in a separate goroutine to avoid blocking the main goroutine
// while reading the compressed data.
func (d *DirectoryOutputHandler) Load(ctx context.Context, target model.Target, output model.Output) error {
	dirPath := config.GetPathAbsoluteToWorkspaceRoot(filepath.Join(target.Label.Package, output.Identifier))

	// Create a temporary file to store the compressed data
	tempFile, err := os.CreateTemp("", fmt.Sprintf("%s_dir_output_*.tar.gz", target.Label.Package))
	if err != nil {
		return err
	}
	tempFilePath := tempFile.Name()
	defer console.WarnDeferredError(ctx, func() error {
		return os.Remove(tempFilePath)
	})
	defer console.WarnDeferredError(ctx, tempFile.Close)

	// Get the compressed file from the cache
	contentReader, err := d.targetCache.LoadFileStream(ctx, target, output)
	if err != nil {
		return err
	}
	defer console.WarnDeferredError(ctx, contentReader.Close)

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
	defer console.WarnDeferredError(ctx, func() error {
		return tempFile.Close()
	})

	// Create the target directory if it doesn't exist
	if err := os.MkdirAll(dirPath, 0755); err != nil {
		return err
	}

	// Create a gzip reader
	gzipReader, err := gzip.NewReader(tempFile)
	if err != nil {
		return err
	}
	defer console.WarnDeferredError(ctx, func() error {
		return gzipReader.Close()
	})

	// Create a tar reader
	tarReader := tar.NewReader(gzipReader)

	// Extract the files
	for {
		header, tarError := tarReader.Next()
		if tarError == io.EOF {
			break
		}
		if tarError != nil {
			return tarError
		}

		// Get the target path
		targetPath := filepath.Join(dirPath, header.Name)

		// Create directories if needed
		if header.Typeflag == tar.TypeDir {
			if err := os.MkdirAll(targetPath, 0755); err != nil {
				return err
			}
			continue
		}

		// Create parent directories if needed
		if err := os.MkdirAll(filepath.Dir(targetPath), 0755); err != nil {
			return err
		}

		// Create the file
		file, tarError := os.OpenFile(targetPath, os.O_CREATE|os.O_WRONLY, os.FileMode(header.Mode))
		if tarError != nil {
			return tarError
		}

		// Copy the content
		if _, err := io.Copy(file, tarReader); err != nil {
			file.Close()
			return err
		}

		if err := file.Close(); err != nil {
			return err
		}
	}

	return nil
}
