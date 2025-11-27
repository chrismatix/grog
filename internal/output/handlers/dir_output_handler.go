package handlers

import (
	"bytes"
	"context"
	"fmt"
	"grog/internal/caching"
	"grog/internal/config"
	"grog/internal/console"
	"grog/internal/hashing"
	"grog/internal/model"
	"grog/internal/proto/gen"
	"io"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"sync/atomic"

	"google.golang.org/protobuf/proto"
)

// DirectoryOutputHandler handles directory outputs by turning them into a merkle tree
// whose definition and file contents are stored in the CAS.
type DirectoryOutputHandler struct {
	cas *caching.Cas
}

// NewDirectoryOutputHandler creates a new DirectoryOutputHandler
func NewDirectoryOutputHandler(cas *caching.Cas) *DirectoryOutputHandler {
	return &DirectoryOutputHandler{
		cas: cas,
	}
}

func (d *DirectoryOutputHandler) Type() HandlerType {
	return DirHandler
}

func (d *DirectoryOutputHandler) Hash(ctx context.Context, target model.Target, output model.Output) (string, error) {
	directoryPath := target.GetAbsOutputPath(output)
	return d.getDirectoryHash(ctx, target, directoryPath)
}

// getDirectoryHash builds a hash tree for the given directory and returns the digest of the tree
func (d *DirectoryOutputHandler) getDirectoryHash(ctx context.Context, target model.Target, directoryPath string) (string, error) {
	logger := console.GetLogger(ctx)

	logger.Debugf("compressing %s (target %s → %s)", directoryPath, target.Label, directoryPath)

	childrenMap := make(map[string]*gen.Directory)
	rootDirectory, _, err := d.writeDirectoryRecursive(ctx, directoryPath, childrenMap)
	if err != nil {
		return "", fmt.Errorf("failed to build hash tree for %s for target %s: %w", directoryPath, target.Label, err)
	}

	children := make([]*gen.Directory, 0, len(childrenMap))
	for _, dir := range childrenMap {
		children = append(children, dir)
	}

	tree := &gen.Tree{
		Root:     rootDirectory,
		Children: children,
	}
	marshalledTree, err := proto.MarshalOptions{Deterministic: true}.Marshal(tree)
	if err != nil {
		return "", fmt.Errorf("failed to marshal tree: %w", err)
	}
	hasher := hashing.GetHasher()
	if _, err := hasher.Write(marshalledTree); err != nil {
		return "", fmt.Errorf("failed to hash tree: %w", err)
	}

	treeDigest := hasher.SumString()
	return treeDigest, nil
}

// Write compresses a directory into <output>.tar.gz and streams it into the cache.
func (d *DirectoryOutputHandler) Write(
	ctx context.Context,
	target model.Target,
	output model.Output,
) (*gen.Output, error) {
	logger := console.GetLogger(ctx)

	directoryPath := target.GetAbsOutputPath(output)
	logger.Debugf("compressing %s (target %s → %s)", directoryPath, target.Label, output)

	childrenMap := make(map[string]*gen.Directory)
	rootDirectory, fileUploads, err := d.writeDirectoryRecursive(ctx, directoryPath, childrenMap)
	if err != nil {
		return nil, fmt.Errorf("failed to build hash tree for %s for target %s: %w", directoryPath, target.Label, err)
	}

	sizeBytes, err := d.uploadFiles(ctx, fileUploads)
	if err != nil {
		return nil, fmt.Errorf("failed to upload directory files to cache: %w", err)
	}

	children := make([]*gen.Directory, 0, len(childrenMap))
	for _, dir := range childrenMap {
		children = append(children, dir)
	}

	tree := &gen.Tree{
		Root:     rootDirectory,
		Children: children,
	}
	marshalledTree, err := proto.MarshalOptions{Deterministic: true}.Marshal(tree)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal tree: %w", err)
	}
	hasher := hashing.GetHasher()
	if _, err := hasher.Write(marshalledTree); err != nil {
		return nil, fmt.Errorf("failed to hash tree: %w", err)
	}

	treeDigest := hasher.SumString()
	err = d.cas.Write(ctx, treeDigest, bytes.NewReader(marshalledTree))
	if err != nil {
		return nil, fmt.Errorf("failed to write tree to cache: %w", err)
	}

	return &gen.Output{
		Kind: &gen.Output_Directory{
			Directory: &gen.DirectoryOutput{
				Path: output.Identifier,
				TreeDigest: &gen.Digest{
					Hash:      treeDigest,
					SizeBytes: sizeBytes,
				},
			},
		},
	}, nil
}

// uploadFiles uploads all files in parallel to the CAS
// the CAS will implement its own concurrency and rate-limiting
func (d *DirectoryOutputHandler) uploadFiles(ctx context.Context, fileUploads []fileUpload) (int64, error) {
	var sizeBytes atomic.Int64

	var wg sync.WaitGroup
	errChan := make(chan error, len(fileUploads))

	for _, uploadAction := range fileUploads {
		wg.Add(1)
		localUploadAction := uploadAction
		go func() {
			defer wg.Done()
			file, err := os.Open(localUploadAction.absolutePath)
			if err != nil {
				errChan <- err
			}
			defer file.Close()

			err = d.cas.Write(ctx, localUploadAction.digest, file)
			if err != nil {
				errChan <- err
			}
			sizeBytes.Add(localUploadAction.size)
		}()
	}

	go func() {
		wg.Wait()
		close(errChan)
	}()

	for err := range errChan {
		if err != nil {
			return 0, err
		}
	}

	return sizeBytes.Load(), nil
}

// Helper struct for recording file that need to be uploaded to the CAS
// in parallel
type fileUpload struct {
	digest       string
	absolutePath string
	size         int64
}

// writeDirectoryRecursive recursively builds a Directory message for the given path
// and writes everything it encounters to the cas
func (d *DirectoryOutputHandler) writeDirectoryRecursive(
	ctx context.Context,
	path string,
	childrenMap map[string]*gen.Directory,
) (*gen.Directory, []fileUpload, error) {
	entries, err := os.ReadDir(path)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to read directory %s: %w", path, err)
	}

	dir := &gen.Directory{
		Files:       []*gen.FileNode{},
		Directories: []*gen.DirectoryNode{},
		Symlinks:    []*gen.SymlinkNode{},
	}

	// Sort entries for deterministic ordering
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].Name() < entries[j].Name()
	})

	var fileUploads []fileUpload

	for _, entry := range entries {
		entryPath := filepath.Join(path, entry.Name())
		info, err := entry.Info()
		if err != nil {
			return nil, nil, fmt.Errorf("failed to get info for %s: %w", entryPath, err)
		}

		switch {
		case info.Mode()&os.ModeSymlink != 0:
			// Handle symlink
			target, err := os.Readlink(entryPath)
			if err != nil {
				return nil, nil, fmt.Errorf("failed to read symlink %s: %w", entryPath, err)
			}
			dir.Symlinks = append(dir.Symlinks, &gen.SymlinkNode{
				Name:   entry.Name(),
				Target: target,
			})

		case entry.IsDir():
			// Handle subdirectory
			subDir, recursiveFileUploads, err := d.writeDirectoryRecursive(ctx, entryPath, childrenMap)
			if err != nil {
				return nil, nil, err
			}

			fileUploads = append(fileUploads, recursiveFileUploads...)

			// Compute digest for the subdirectory
			digest, err := computeDirectoryDigest(subDir)
			if err != nil {
				return nil, nil, fmt.Errorf("failed to compute digest for directory %s: %w", entryPath, err)
			}

			dir.Directories = append(dir.Directories, &gen.DirectoryNode{
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
				return nil, nil, fmt.Errorf("failed to compute digest for file %s: %w", entryPath, err)
			}

			fileUploads = append(fileUploads, fileUpload{digest.Hash, entryPath, digest.GetSizeBytes()})

			dir.Files = append(dir.Files, &gen.FileNode{
				Name:         entry.Name(),
				Digest:       digest,
				IsExecutable: info.Mode()&0111 != 0,
			})
		}
	}

	return dir, fileUploads, nil
}

// computeDirectoryDigest computes the digest for a Directory message.
func computeDirectoryDigest(dir *gen.Directory) (*gen.Digest, error) {
	// Serialize the directory to bytes
	data, err := proto.MarshalOptions{Deterministic: true}.Marshal(dir)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal directory: %w", err)
	}

	// Compute hash
	hasher := hashing.GetHasher()
	if _, err := hasher.Write(data); err != nil {
		return nil, fmt.Errorf("failed to hash directory: %w", err)
	}

	return &gen.Digest{
		Hash:      hasher.SumString(),
		SizeBytes: int64(len(data)),
	}, nil
}

// computeFileDigest computes the digest for a file.
func computeFileDigest(path string) (*gen.Digest, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("failed to open file %s: %w", path, err)
	}
	defer file.Close()

	hasher := hashing.GetHasher()
	size, err := io.Copy(hasher, file)
	if err != nil {
		return nil, fmt.Errorf("failed to hash file %s: %w", path, err)
	}

	return &gen.Digest{
		Hash:      hasher.SumString(),
		SizeBytes: size,
	}, nil
}

// Load fetches the tree from the CAS and then fetches all files from the cache
func (d *DirectoryOutputHandler) Load(ctx context.Context, target model.Target, output *gen.Output) error {
	logger := console.GetLogger(ctx)
	dirPath := config.GetPathAbsoluteToWorkspaceRoot(filepath.Join(target.Label.Package, output.GetDirectory().GetPath()))

	logger.Debugf("loading directory from cache for target %s → %s", target.Label, dirPath)

	// Fetch the tree from CAS
	treeDigest := output.GetDirectory().GetTreeDigest().Hash

	// Check the current directory hash against the cached tree
	// so that we can avoid downloading the directory if it hasn't changed
	localDirectoryDigest, err := d.getDirectoryHash(ctx, target, dirPath)
	if err == nil && treeDigest == localDirectoryDigest {
		logger.Debugf("directory %s already exists locally so skipping load", dirPath)
		return nil
	}

	treeBytes, err := d.cas.LoadBytes(ctx, treeDigest)
	if err != nil {
		return fmt.Errorf("failed to read tree from cache: %w", err)
	}
	// Unmarshal the tree
	tree := &gen.Tree{}
	if err := proto.Unmarshal(treeBytes, tree); err != nil {
		return fmt.Errorf("failed to unmarshal tree: %w", err)
	}

	// Build a map of children directories by digest for easy lookup
	childrenMap := make(map[string]*gen.Directory)
	for _, child := range tree.Children {
		digest, err := computeDirectoryDigest(child)
		if err != nil {
			return fmt.Errorf("failed to compute child directory digest: %w", err)
		}
		childrenMap[digest.Hash] = child
	}

	// Remove the directory if it already exists
	if err := os.RemoveAll(dirPath); err != nil {
		return fmt.Errorf("failed to remove directory %s: %w", dirPath, err)
	}

	// Create the root directory
	if err := os.MkdirAll(dirPath, 0755); err != nil {
		return fmt.Errorf("failed to create directory %s: %w", dirPath, err)
	}

	// WaitGroup to wait for all goroutines to finish
	var waitGroup sync.WaitGroup
	errChan := make(chan error, len(tree.Children))
	// Recursively load the directory structure
	if err := d.loadDirectoryRecursive(ctx, dirPath, tree.Root, childrenMap, &waitGroup, errChan); err != nil {
		return fmt.Errorf("failed to load directory structure: %w", err)
	}

	// Wait for all goroutines to finish
	waitGroup.Wait()
	close(errChan)
	for err := range errChan {
		if err != nil {
			return err
		}
	}

	return nil
}

// loadDirectoryRecursive recursively reconstructs a directory from the Directory message
func (d *DirectoryOutputHandler) loadDirectoryRecursive(
	ctx context.Context,
	path string,
	dir *gen.Directory,
	childrenMap map[string]*gen.Directory,
	waitGroup *sync.WaitGroup,
	errChan chan error,
) error {
	// Create all files
	for _, fileNode := range dir.Files {
		filePath := filepath.Join(path, fileNode.Name)

		// Fetch file contents from CAS
		go func() {
			waitGroup.Add(1)
			defer waitGroup.Done()
			err := d.downloadFile(ctx, fileNode.Digest.Hash, filePath, fileNode.IsExecutable)
			if err != nil {
				errChan <- fmt.Errorf("failed to download file %s: %v", filePath, err)
			}
		}()
	}

	// Create all subdirectories
	for _, dirNode := range dir.Directories {
		subDirPath := filepath.Join(path, dirNode.Name)

		// Create the subdirectory
		if err := os.MkdirAll(subDirPath, 0755); err != nil {
			return fmt.Errorf("failed to create directory %s: %w", subDirPath, err)
		}

		// Get the child directory from the map
		childDir, exists := childrenMap[dirNode.Digest.Hash]
		if !exists {
			return fmt.Errorf("child directory %s not found in children map", dirNode.Name)
		}

		// Recursively load the subdirectory
		if err := d.loadDirectoryRecursive(ctx, subDirPath, childDir, childrenMap, waitGroup, errChan); err != nil {
			return err
		}
	}

	// Create all symlinks
	for _, symlinkNode := range dir.Symlinks {
		symlinkPath := filepath.Join(path, symlinkNode.Name)

		// Create the symlink
		if err := os.Symlink(symlinkNode.Target, symlinkPath); err != nil {
			return fmt.Errorf("failed to create symlink %s: %w", symlinkPath, err)
		}
	}

	return nil
}

func (d *DirectoryOutputHandler) downloadFile(ctx context.Context, digest, localPath string, isExecutable bool) error {
	// Fetch file contents from CAS
	fileReader, err := d.cas.Load(ctx, digest)
	if err != nil {
		return fmt.Errorf("failed to read file %s from cache: %w", localPath, err)
	}

	file, err := os.Create(localPath)
	if err != nil {
		return fmt.Errorf("failed to create file %s: %w", localPath, err)
	}
	if _, err := io.Copy(file, fileReader); err != nil {
		return fmt.Errorf("failed to write file %s: %w", localPath, err)
	}

	// Write file
	mode := os.FileMode(0644)
	if isExecutable {
		mode = 0755
	}
	err = file.Chmod(mode)
	if err != nil {
		return fmt.Errorf("failed to chmod file %s: %w", localPath, err)
	}
	return nil
}
