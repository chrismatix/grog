package handlers

import (
	"context"
	"fmt"
	"grog/internal/caching"
	"grog/internal/config"
	"grog/internal/model"
	"io"
	"os"
	"path/filepath"

	"github.com/cespare/xxhash/v2"
)

// FileOutputHandler is the default output handler that writes files to the file system.
// mostly passes directly through to the target cache which handles files
type FileOutputHandler struct {
	targetCache *caching.TargetCache
}

func NewFileOutputHandler(targetCache *caching.TargetCache) *FileOutputHandler {
	return &FileOutputHandler{
		targetCache: targetCache,
	}
}

func (f *FileOutputHandler) Type() HandlerType {
	return "file"
}

func (f *FileOutputHandler) Has(ctx context.Context, target model.Target, output model.Output) (bool, error) {
	return f.targetCache.FileExists(ctx, target, output)
}

func (f *FileOutputHandler) Write(ctx context.Context, target model.Target, output model.Output) (string, error) {
	absOutputPath := config.GetPathAbsoluteToWorkspaceRoot(filepath.Join(target.Label.Package, output.Identifier))
	file, err := os.Open(absOutputPath)
	if err != nil {
		return "", fmt.Errorf("declared output %s for target %s was not created", output, target.Label)
	}
	defer file.Close()

	hasher := xxhash.New()
	tee := io.TeeReader(file, hasher)

	if err := f.targetCache.WriteFileStream(ctx, target, output, tee); err != nil {
		return "", err
	}

	return fmt.Sprintf("%x", hasher.Sum64()), nil
}

func (f *FileOutputHandler) Load(ctx context.Context, target model.Target, output model.Output) (string, error) {
	absOutputPath := config.GetPathAbsoluteToWorkspaceRoot(filepath.Join(target.Label.Package, output.Identifier))
	contentReader, err := f.targetCache.LoadFileStream(ctx, target, output)
	if err != nil {
		return "", err
	}
	defer contentReader.Close()

	outputFile, err := os.Create(absOutputPath)
	if err != nil {
		return "", err
	}

	hasher := xxhash.New()
	if _, err := io.Copy(outputFile, io.TeeReader(contentReader, hasher)); err != nil {
		outputFile.Close()
		return "", err
	}

	if err := outputFile.Close(); err != nil {
		return "", err
	}

	return fmt.Sprintf("%x", hasher.Sum64()), nil
}
