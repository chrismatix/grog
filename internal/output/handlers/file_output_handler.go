package handlers

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"grog/internal/caching"
	"grog/internal/config"
	"grog/internal/hashing"
	"grog/internal/model"
	"grog/internal/proto/gen"
	"grog/internal/worker"
)

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

func (f *FileOutputHandler) Write(ctx context.Context, target model.Target, output model.Output, tracker *worker.ProgressTracker, update worker.StatusFunc) (*gen.Output, error) {
	relativePath := output.Identifier
	absOutputPath := target.GetAbsOutputPath(output)

	file, err := os.Open(absOutputPath)
	if err != nil {
		return nil, fmt.Errorf("declared output %s for target %s was not created", output, target.Label)
	}
	defer file.Close()

	fileHash, err := hashing.HashFile(absOutputPath)
	if err != nil {
		return nil, fmt.Errorf("failed to hash file %s: %w", absOutputPath, err)
	}

	fileInfo, err := file.Stat()
	if err != nil {
		return nil, fmt.Errorf("failed to get file size %s: %w", absOutputPath, err)
	}

	progress := tracker
	if progress != nil {
		progress = progress.SubTracker(fmt.Sprintf("%s: writing %s", target.Label, relativePath), fileInfo.Size())
	}

	reader := io.Reader(file)
	if progress != nil {
		reader = progress.WrapReader(file)
	}

	if err := f.cas.Write(ctx, fileHash, reader); err != nil {
		return nil, err
	}

	if progress != nil {
		progress.Complete()
	}

	return &gen.Output{
		Kind: &gen.Output_File{
			File: &gen.FileOutput{
				Path: relativePath,
				Digest: &gen.Digest{
					Hash:      fileHash,
					SizeBytes: fileInfo.Size(),
				},
			},
		},
	}, nil
}

func (f *FileOutputHandler) Load(ctx context.Context, target model.Target, output *gen.Output, tracker *worker.ProgressTracker, update worker.StatusFunc) error {
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
