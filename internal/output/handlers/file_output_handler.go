package handlers

import (
	"context"
	"fmt"
	"grog/internal/caching"
	"grog/internal/config"
	"grog/internal/hashing"
	"grog/internal/model"
	"grog/internal/proto/gen"
	"io"
	"os"
	"path/filepath"
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

func (f *FileOutputHandler) Write(ctx context.Context, target model.Target, output model.Output) (*gen.Output, error) {
	relativePath := output.Identifier
	absOutputPath := config.GetPathAbsoluteToWorkspaceRoot(filepath.Join(target.Label.Package, relativePath))
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

	if err := f.cas.Write(ctx, fileHash, file); err != nil {
		return nil, err
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

func (f *FileOutputHandler) Load(ctx context.Context, target model.Target, output *gen.Output) error {
	absOutputPath := config.GetPathAbsoluteToWorkspaceRoot(filepath.Join(target.Label.Package, output.GetFile().GetPath()))
	contentReader, err := f.cas.Load(ctx, output.GetFile().GetDigest().GetHash())
	if err != nil {
		return err
	}
	defer contentReader.Close()

	outputFile, err := os.Create(absOutputPath)
	if err != nil {
		return err
	}

	if _, err := io.Copy(outputFile, contentReader); err != nil {
		return err
	}

	if err := outputFile.Close(); err != nil {
		return err
	}

	return nil
}
