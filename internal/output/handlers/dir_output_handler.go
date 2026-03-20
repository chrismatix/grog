package handlers

import (
	"bytes"
	"context"
	"fmt"
	"grog/internal/hashing"

	"io"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"sync/atomic"

	"grog/internal/caching"
	"grog/internal/config"
	"grog/internal/console"
	"grog/internal/model"
	"grog/internal/proto/gen"
	"grog/internal/worker"

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

	logger.Debugf("hashing directory %s (target %s)", directoryPath, target.Label)

	childrenMap := make(map[string]*gen.Directory)
	rootDirectory, _, err := d.writeDirectoryRecursive(ctx, directoryPath, childrenMap)
	if err != nil {
		return "", fmt.Errorf("failed to build hash tree for %s for target %s: %w", directoryPath, target.Label, err)
	}

	children := getSortedChildren(childrenMap)

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

func getSortedChildren(childrenMap map[string]*gen.Directory) []*gen.Directory {
	// Sort children by digest to ensure deterministic tree construction
	childDigests := make([]string, 0, len(childrenMap))
	for digest := range childrenMap {
		childDigests = append(childDigests, digest)
	}
	sort.Strings(childDigests)

	children := make([]*gen.Directory, 0, len(childrenMap))
	for _, digest := range childDigests {
		children = append(children, childrenMap[digest])
	}
	return children
}

// Write compresses a directory into <output>.tar.gz and streams it into the cache.
func (d *DirectoryOutputHandler) Write(
	ctx context.Context,
	target model.Target,
	output model.Output,
	tracker *worker.ProgressTracker,
) (*gen.Output, error) {
	logger := console.GetLogger(ctx)

	directoryPath := target.GetAbsOutputPath(output)
	logger.Debugf("compressing %s (target %s → %s)", directoryPath, target.Label, output)

	childrenMap := make(map[string]*gen.Directory)
	rootDirectory, fileUploads, err := d.writeDirectoryRecursive(ctx, directoryPath, childrenMap)
	if err != nil {
		return nil, fmt.Errorf("failed to build hash tree for %s for target %s: %w", directoryPath, target.Label, err)
	}

	children := getSortedChildren(childrenMap)

	tree := &gen.Tree{
		Root:     rootDirectory,
		Children: children,
	}
	marshalledTree, err := proto.MarshalOptions{Deterministic: true}.Marshal(tree)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal tree: %w", err)
	}

	treeDigest := hashing.HashBytes(marshalledTree)

	totalBytes := int64(0)
	for _, upload := range fileUploads {
		totalBytes += upload.sizeBytes
	}

	progress := tracker
	if progress != nil {
		progress = progress.SubTracker(
			fmt.Sprintf("%s: writing directory %s", target.Label, output.Identifier),
			totalBytes,
		)
	}

	sizeBytes, err := d.uploadFiles(ctx, fileUploads, progress)
	if err != nil {
		return nil, fmt.Errorf("failed to upload directory files to cache: %w", err)
	}

	logger.Debugf("writing directory tree digest %s for %s", treeDigest, directoryPath)
	err = d.cas.Write(ctx, treeDigest, bytes.NewReader(marshalledTree))

	if err != nil {
		return nil, fmt.Errorf("failed to write tree to cache: %w", err)
	}

	if progress != nil {
		progress.Complete()
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
func (d *DirectoryOutputHandler) uploadFiles(ctx context.Context, fileUploads []fileUpload, progress *worker.ProgressTracker) (int64, error) {
	logger := console.GetLogger(ctx)
	var sizeBytes atomic.Int64

	var wg sync.WaitGroup
	errChan := make(chan error, len(fileUploads))

	for _, uploadAction := range fileUploads {
		wg.Add(1)
		localUploadAction := uploadAction
		go func() {
			defer wg.Done()
			logger.Debugf("uploading %s to CAS digest %s", localUploadAction.absolutePath, localUploadAction.digest)
			file, err := os.Open(localUploadAction.absolutePath)
			if err != nil {
				errChan <- err
			}
			defer file.Close()

			reader := io.Reader(file)
			if progress != nil {
				reader = progress.WrapReader(file)
			}

			err = d.cas.Write(ctx, localUploadAction.digest, reader)
			if err != nil {
				errChan <- err
			}
			sizeBytes.Add(localUploadAction.sizeBytes)
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
	sizeBytes    int64
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

	return &gen.Digest{
		Hash:      hashing.HashBytes(data),
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
func (d *DirectoryOutputHandler) Load(
	ctx context.Context,
	target model.Target,
	output *gen.Output,
	tracker *worker.ProgressTracker,
) error {
	logger := console.GetLogger(ctx)
	directoryOutput := output.GetDirectory()
	dirPath := config.GetPathAbsoluteToWorkspaceRoot(filepath.Join(target.Label.Package, directoryOutput.GetPath()))

	logger.Debugf("loading directory from cache for target %s → %s", target.Label, dirPath)

	// Fetch the tree from CAS
	treeDigest := directoryOutput.GetTreeDigest().Hash

	// Check the current directory hash against the cached tree
	// so that we can avoid downloading the directory if it hasn't changed
	localDirectoryDigest, err := d.getDirectoryHash(ctx, target, dirPath)
	if err == nil && treeDigest == localDirectoryDigest {
		logger.Debugf("directory %s already exists locally so skipping load", dirPath)
		return nil
	} else if err != nil {
		logger.Debugf("failed to check directory hash: %v", err)
	} else {
		logger.Debugf("directory %s does not exist locally or has changed so reloading", dirPath)
	}

	logger.Debugf("loading directory tree digest %s for %s", treeDigest, dirPath)
	treeBytes, err := d.cas.LoadBytes(ctx, treeDigest)
	if err != nil {
		return fmt.Errorf("failed to read tree from cache: %w", err)
	}

	totalBytes := directoryOutput.GetTreeDigest().GetSizeBytes()
	progress := tracker
	if progress != nil {
		progress = progress.SubTracker(
			fmt.Sprintf("%s: loading directory %s", target.Label, directoryOutput.GetPath()),
			totalBytes,
		)
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
	if err := d.loadDirectoryRecursive(ctx, dirPath, tree.Root, childrenMap, progress, &waitGroup, errChan); err != nil {
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

	if progress != nil {
		progress.Complete()
	}

	return nil
}

// loadDirectoryRecursive recursively reconstructs a directory from the Directory message
func (d *DirectoryOutputHandler) loadDirectoryRecursive(
	ctx context.Context,
	path string,
	dir *gen.Directory,
	childrenMap map[string]*gen.Directory,
	progress *worker.ProgressTracker,
	waitGroup *sync.WaitGroup,
	errChan chan error,
) error {
	// Create all files
	for _, fileNode := range dir.Files {
		filePath := filepath.Join(path, fileNode.Name)

		waitGroup.Add(1)
		// Fetch file contents from CAS
		go func(filePath string, digest string) {
			defer waitGroup.Done()
			console.GetLogger(ctx).Debugf("loading file for directory output %s from digest %s", filePath, digest)
			err := d.downloadFile(ctx, digest, filePath, fileNode.IsExecutable, progress)
			if err != nil {
				errChan <- fmt.Errorf("failed to download file %s: %v", filePath, err)
			}
		}(filePath, fileNode.Digest.Hash)
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
		if err := d.loadDirectoryRecursive(ctx, subDirPath, childDir, childrenMap, progress, waitGroup, errChan); err != nil {
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

func (d *DirectoryOutputHandler) downloadFile(ctx context.Context, digest, localPath string, isExecutable bool, progress *worker.ProgressTracker) error {
	// Fetch file contents from CAS
	fileReader, err := d.cas.Load(ctx, digest)
	if err != nil {
		return fmt.Errorf("failed to read file %s from cache: %w", localPath, err)
	}
	defer fileReader.Close()

	file, err := os.Create(localPath)
	if err != nil {
		return fmt.Errorf("failed to create file %s: %w", localPath, err)
	}
	reader := io.Reader(fileReader)
	if progress != nil {
		reader = progress.WrapReader(fileReader)
	}
	if _, err := io.Copy(file, reader); err != nil {
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
