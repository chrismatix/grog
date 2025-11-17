package handlers

import (
	"context"
	"grog/internal/archive"
	"grog/internal/caching"
	"grog/internal/config"
	"grog/internal/console"
	"grog/internal/model"
	"io"
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

// Write compresses a directory into <output>.tar.gz and streams it into the cache.
func (d *DirectoryOutputHandler) Write(
	ctx context.Context,
	target model.Target,
	output model.Output,
) error {
	logger := console.GetLogger(ctx)

	workspaceRelativePath := filepath.Join(target.Label.Package, output.Identifier)
	directoryPath := config.GetPathAbsoluteToWorkspaceRoot(workspaceRelativePath)

	logger.Debugf("compressing %s (target %s → %s)", directoryPath, target.Label, output)

	pipeReader, pipeWriter := io.Pipe()
	errChan := make(chan error, 1)

	go func() {
		errChan <- archive.TarGzipDirectory(ctx, directoryPath, pipeWriter)
		pipeWriter.Close()
	}()

	logger.Debug("streaming tar.gz to cache")
	writeErr := d.targetCache.WriteFileStream(ctx, target, output, pipeReader)
	// Close the reader in case WriteFileStream returns early to unblock writer goroutine
	pipeReader.Close()
	// Wait for the writing goroutine to finish and capture its error
	goroutineErr := <-errChan
	if writeErr != nil {
		return writeErr
	}
	return goroutineErr
}

func (d *DirectoryOutputHandler) Load(ctx context.Context, target model.Target, output model.Output) error {
	logger := console.GetLogger(ctx)
	dirPath := config.GetPathAbsoluteToWorkspaceRoot(filepath.Join(target.Label.Package, output.Identifier))

	contentReader, err := d.targetCache.LoadFileStream(ctx, target, output)
	if err != nil {
		return err
	}
	defer console.WarnOnError(ctx, contentReader.Close)

	fileCount, err := archive.ExtractTarGzip(ctx, contentReader, dirPath)
	if err != nil {
		return err
	}

	logger.Debugf("Successfully extracted %d files to %s", fileCount, dirPath)
	return nil
}
