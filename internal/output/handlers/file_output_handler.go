package handlers

import (
	"context"
	"fmt"
	"grog/internal/caching"
	"grog/internal/config"
	"grog/internal/model"
	v1 "grog/internal/proto/gen"
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

func (f *FileOutputHandler) Has(ctx context.Context, output *v1.FileOutput) (bool, error) {
	return f.cas.Exists(ctx, output.Digest.Hash)
}

func (f *FileOutputHandler) Write(ctx context.Context, target model.Target, output *v1.Output) error {
	absOutputPath := config.GetPathAbsoluteToWorkspaceRoot(filepath.Join(target.Label.Package, output.GetFile().Path))
	file, err := os.Open(absOutputPath)
	if err != nil {
		return fmt.Errorf("declared output %s for target %s was not created", output, target.Label)
	}
	defer file.Close()

	if err := f.cas.Write(ctx, output.GetFile().Digest.GetHash(), file); err != nil {
		return err
	}

	return nil
}

func (f *FileOutputHandler) Load(ctx context.Context, target model.Target, output *v1.Output) error {
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

	if err := outputFile.Close(); err != nil {
		return err
	}

	return nil
}
