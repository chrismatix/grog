package handlers

import (
	"context"
	"grog/internal/caching"
	"grog/internal/model"
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

func (f *FileOutputHandler) Write(ctx context.Context, target model.Target, output model.Output) error {
	return f.targetCache.WriteFile(ctx, target, output)
}

func (f *FileOutputHandler) Load(ctx context.Context, target model.Target, output model.Output) error {
	return f.targetCache.LoadFile(ctx, target, output)
}
