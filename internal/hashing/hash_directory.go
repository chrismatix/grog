package hashing

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"

	pb "grog/internal/proto/gen"

	"github.com/cespare/xxhash/v2"
	"google.golang.org/protobuf/proto"
)

// HashDirectory takes a path to a directory and returns the Merkle Tree representation.
func HashDirectory(path string) (*pb.Tree, error) {
	// Map to store all directories by their digest
	childrenMap := make(map[string]*pb.Directory)

	// Build the root directory
	root, err := buildDirectory(path, childrenMap)
	if err != nil {
		return nil, fmt.Errorf("failed to build root directory: %w", err)
	}

	// Flatten the children map into a slice
	children := make([]*pb.Directory, 0, len(childrenMap))
	for _, dir := range childrenMap {
		children = append(children, dir)
	}

	return &pb.Tree{
		Root:     root,
		Children: children,
	}, nil
}

// buildDirectory recursively builds a Directory message for the given path.
func buildDirectory(path string, childrenMap map[string]*pb.Directory) (*pb.Directory, error) {
	entries, err := os.ReadDir(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read directory %s: %w", path, err)
	}

	dir := &pb.Directory{
		Files:       []*pb.FileNode{},
		Directories: []*pb.DirectoryNode{},
		Symlinks:    []*pb.SymlinkNode{},
	}

	// Sort entries for deterministic ordering
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].Name() < entries[j].Name()
	})

	for _, entry := range entries {
		entryPath := filepath.Join(path, entry.Name())
		info, err := entry.Info()
		if err != nil {
			return nil, fmt.Errorf("failed to get info for %s: %w", entryPath, err)
		}

		switch {
		case info.Mode()&os.ModeSymlink != 0:
			// Handle symlink
			target, err := os.Readlink(entryPath)
			if err != nil {
				return nil, fmt.Errorf("failed to read symlink %s: %w", entryPath, err)
			}
			dir.Symlinks = append(dir.Symlinks, &pb.SymlinkNode{
				Name:   entry.Name(),
				Target: target,
			})

		case entry.IsDir():
			// Handle subdirectory
			subDir, err := buildDirectory(entryPath, childrenMap)
			if err != nil {
				return nil, err
			}

			// Compute digest for the subdirectory
			digest, err := computeDirectoryDigest(subDir)
			if err != nil {
				return nil, fmt.Errorf("failed to compute digest for directory %s: %w", entryPath, err)
			}

			dir.Directories = append(dir.Directories, &pb.DirectoryNode{
				Name:   entry.Name(),
				Digest: digest,
			})

			// Add to children map
			digestStr := digest.Hash
			if _, exists := childrenMap[digestStr]; !exists {
				childrenMap[digestStr] = subDir
			}

		default:
			// Handle regular file
			digest, err := computeFileDigest(entryPath)
			if err != nil {
				return nil, fmt.Errorf("failed to compute digest for file %s: %w", entryPath, err)
			}

			dir.Files = append(dir.Files, &pb.FileNode{
				Name:         entry.Name(),
				Digest:       digest,
				IsExecutable: info.Mode()&0111 != 0,
			})
		}
	}

	return dir, nil
}

// computeDirectoryDigest computes the digest for a Directory message.
func computeDirectoryDigest(dir *pb.Directory) (*pb.Digest, error) {
	// Serialize the directory to bytes
	data, err := proto.Marshal(dir)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal directory: %w", err)
	}

	// Compute xxhash hash
	hasher := xxhash.New()
	if _, err := hasher.Write(data); err != nil {
		return nil, fmt.Errorf("failed to hash directory: %w", err)
	}

	return &pb.Digest{
		Hash:      fmt.Sprintf("%x", hasher.Sum64()),
		SizeBytes: int64(len(data)),
	}, nil
}

// computeFileDigest computes the digest for a file.
func computeFileDigest(path string) (*pb.Digest, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("failed to open file %s: %w", path, err)
	}
	defer file.Close()

	hasher := xxhash.New()
	size, err := io.Copy(hasher, file)
	if err != nil {
		return nil, fmt.Errorf("failed to hash file %s: %w", path, err)
	}

	return &pb.Digest{
		Hash:      fmt.Sprintf("%x", hasher.Sum64()),
		SizeBytes: size,
	}, nil
}
