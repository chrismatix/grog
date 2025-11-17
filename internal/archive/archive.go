package archive

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// TarGzipDirectory walks the source directory and writes a tar.gz archive to the provided writer.
// Symlinks are preserved and stored using PAX headers.
func TarGzipDirectory(ctx context.Context, src string, w io.Writer) error {
	gzipWriter := gzip.NewWriter(w)
	defer gzipWriter.Close()

	tarWriter := tar.NewWriter(gzipWriter)
	defer tarWriter.Close()

	return filepath.WalkDir(src, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}

		if path == src {
			return nil
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		info, err := entry.Info()
		if err != nil {
			return err
		}

		relativePath, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}

		var linkTarget string
		if info.Mode()&os.ModeSymlink != 0 {
			linkTarget, err = os.Readlink(path)
			if err != nil {
				return err
			}
		}

		header, err := tar.FileInfoHeader(info, linkTarget)
		if err != nil {
			return err
		}

		header.Name = filepath.ToSlash(relativePath)
		header.Format = tar.FormatPAX

		if err := tarWriter.WriteHeader(header); err != nil {
			return err
		}

		if entry.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return nil
		}

		file, err := os.Open(path)
		if err != nil {
			return err
		}
		defer file.Close()

		if _, err := io.Copy(tarWriter, file); err != nil {
			return err
		}

		return nil
	})
}

// ExtractTarGzip extracts a tar.gz archive into destDir, restoring files, directories, and symlinks.
// It returns the number of filesystem entries created.
func ExtractTarGzip(ctx context.Context, r io.Reader, destDir string) (int, error) {
	if err := os.MkdirAll(destDir, 0o755); err != nil {
		return 0, err
	}

	gzipReader, err := gzip.NewReader(r)
	if err != nil {
		return 0, err
	}
	defer gzipReader.Close()

	tarReader := tar.NewReader(gzipReader)
	fileCount := 0

	for {
		header, tarErr := tarReader.Next()
		if tarErr == io.EOF {
			break
		}
		if tarErr != nil {
			return fileCount, fmt.Errorf("error reading tar header: %w", tarErr)
		}

		select {
		case <-ctx.Done():
			return fileCount, ctx.Err()
		default:
		}

		targetPath := filepath.Join(destDir, filepath.FromSlash(header.Name))
		if !strings.HasPrefix(targetPath, filepath.Clean(destDir)+string(filepath.Separator)) && filepath.Clean(targetPath) != filepath.Clean(destDir) {
			return fileCount, fmt.Errorf("tar entry escapes destination: %s", header.Name)
		}

		switch header.Typeflag {
		case tar.TypeDir:
			if err := os.RemoveAll(targetPath); err != nil {
				return fileCount, err
			}
			if err := os.MkdirAll(targetPath, 0o755); err != nil {
				return fileCount, err
			}
			fileCount++

		case tar.TypeSymlink:
			if err := os.MkdirAll(filepath.Dir(targetPath), 0o755); err != nil {
				return fileCount, fmt.Errorf("failed to create parent directories for symlink %s: %w", targetPath, err)
			}
			if err := os.RemoveAll(targetPath); err != nil {
				return fileCount, fmt.Errorf("failed to remove existing path for symlink %s: %w", targetPath, err)
			}
			if err := os.Symlink(header.Linkname, targetPath); err != nil {
				return fileCount, fmt.Errorf("failed to create symlink %s -> %s: %w", targetPath, header.Linkname, err)
			}
			fileCount++

		default:
			if err := os.MkdirAll(filepath.Dir(targetPath), 0o755); err != nil {
				return fileCount, fmt.Errorf("failed to create parent directories for file %s: %w", targetPath, err)
			}

			file, openErr := os.OpenFile(targetPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, os.FileMode(header.Mode))
			if openErr != nil {
				return fileCount, fmt.Errorf("failed to create file %s: %w", targetPath, openErr)
			}

			if _, err := io.Copy(file, tarReader); err != nil {
				file.Close()
				return fileCount, fmt.Errorf("failed to copy file %s: %w", targetPath, err)
			}

			if err := file.Close(); err != nil {
				return fileCount, fmt.Errorf("failed to close file %s: %w", targetPath, err)
			}
			fileCount++
		}
	}

	return fileCount, nil
}
