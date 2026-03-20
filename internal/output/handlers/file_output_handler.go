package handlers

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"grog/internal/caching"
	"grog/internal/config"
	"grog/internal/console"
	"grog/internal/hashing"
	"grog/internal/model"
	"grog/internal/proto/gen"
	"grog/internal/worker"
)

type fileWritePlan struct {
	cas          *caching.Cas
	stagedPath   string
	digest       string
	sizeBytes    int64
	targetLabel  string
	relativePath string
}

func (f *fileWritePlan) Upload(ctx context.Context, tracker *worker.ProgressTracker) error {
	file, err := os.Open(f.stagedPath)
	if err != nil {
		return fmt.Errorf("failed to open staged file %s for cache write: %w", f.stagedPath, err)
	}
	defer file.Close()

	progress := tracker
	if progress != nil {
		progress = progress.SubTracker(fmt.Sprintf("%s: writing %s", f.targetLabel, f.relativePath), f.sizeBytes)
	}

	reader := io.Reader(file)
	if progress != nil {
		reader = progress.WrapReader(file)
	}

	console.GetLogger(ctx).Debugf("writing staged file output %s with digest %s", f.stagedPath, f.digest)
	if err := f.cas.Write(ctx, f.digest, reader); err != nil {
		return err
	}

	if progress != nil {
		progress.Complete()
	}
	return nil
}

func (f *fileWritePlan) Cleanup(_ context.Context) error {
	return os.Remove(f.stagedPath)
}

// FileOutputHandler is the default output handler that writes files to the file system.
// mostly passes directly through to the target cache which handles files
type FileOutputHandler struct {
	cas *caching.Cas
}

func NewFileOutputHandler(cas *caching.Cas) *FileOutputHandler {
	return &FileOutputHandler{
		cas: cas,
	}
}

func (f *FileOutputHandler) Type() HandlerType {
	return "file"
}

func (f *FileOutputHandler) Hash(_ context.Context, target model.Target, output model.Output) (string, error) {
	absOutputPath := target.GetAbsOutputPath(output)
	fileHash, err := hashing.HashFile(absOutputPath)
	if err != nil {
		return "", fmt.Errorf("failed to hash file %s: %w", absOutputPath, err)
	}
	return fileHash, nil
}

func (f *FileOutputHandler) Write(
	ctx context.Context,
	target model.Target,
	output model.Output,
	_ *worker.ProgressTracker,
) (*PreparedOutput, error) {
	relativePath := output.Identifier
	absOutputPath := target.GetAbsOutputPath(output)

	file, err := os.Open(absOutputPath)
	if err != nil {
		return nil, fmt.Errorf("declared output %s for target %s was not created", output, target.Label)
	}
	defer file.Close()

	stagedFile, err := os.CreateTemp("", "grog-cache-file-*")
	if err != nil {
		return nil, fmt.Errorf("failed to create staged file for %s: %w", absOutputPath, err)
	}
	stagedPath := stagedFile.Name()
	cleanupStagedFile := true
	defer func() {
		if cleanupStagedFile {
			_ = os.Remove(stagedPath)
		}
	}()

	hasher := hashing.GetHasher()
	sizeBytes, err := io.Copy(io.MultiWriter(stagedFile, hasher), file)
	if closeErr := stagedFile.Close(); closeErr != nil && err == nil {
		err = closeErr
	}
	if err != nil {
		return nil, fmt.Errorf("failed to stage file %s for cache write: %w", absOutputPath, err)
	}

	fileHash := hasher.SumString()

	genOutput := &gen.Output{
		Kind: &gen.Output_File{
			File: &gen.FileOutput{
				Path: relativePath,
				Digest: &gen.Digest{
					Hash:      fileHash,
					SizeBytes: sizeBytes,
				},
			},
		},
	}

	writePlan := &fileWritePlan{
		cas:          f.cas,
		stagedPath:   stagedPath,
		digest:       fileHash,
		sizeBytes:    sizeBytes,
		targetLabel:  target.Label.String(),
		relativePath: relativePath,
	}
	cleanupStagedFile = false

	return &PreparedOutput{
		Output:    genOutput,
		WritePlan: writePlan,
	}, nil
}

func (f *FileOutputHandler) Load(
	ctx context.Context,
	target model.Target,
	output *gen.Output,
	tracker *worker.ProgressTracker,
) error {
	absOutputPath := config.GetPathAbsoluteToWorkspaceRoot(filepath.Join(target.Label.Package, output.GetFile().GetPath()))
	existingHash, err := hashing.HashFile(absOutputPath)

	// If the local hash is the same as the cached one we don't need to
	// load the file from the CAS
	if err == nil && existingHash == output.GetFile().GetDigest().GetHash() {
		return nil
	}

	progress := tracker
	if progress != nil {
		progress = progress.SubTracker(
			fmt.Sprintf("%s: loading %s", target.Label, output.GetFile().GetPath()),
			output.GetFile().GetDigest().GetSizeBytes(),
		)
	}

	console.GetLogger(ctx).Debugf("loading file output %s with digest %s", absOutputPath, output.GetFile().GetDigest().GetHash())
	contentReader, err := f.cas.Load(ctx, output.GetFile().GetDigest().GetHash())
	if err != nil {
		return err
	}
	defer contentReader.Close()
	reader := io.Reader(contentReader)
	if progress != nil {
		reader = progress.WrapReader(contentReader)
	}

	outputFile, err := os.Create(absOutputPath)
	if err != nil {
		return err
	}

	if _, err := io.Copy(outputFile, reader); err != nil {
		return err
	}

	if progress != nil {
		progress.Complete()
	}

	if err := outputFile.Close(); err != nil {
		return err
	}

	return nil
}
