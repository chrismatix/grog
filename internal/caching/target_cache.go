package caching

import (
	"bytes"
	"context"
	"fmt"
	"grog/internal/caching/backends"
	"grog/internal/config"
	"grog/internal/hashing"
	"grog/internal/model"
	"io"
	"os"
	"path/filepath"
)

// existsFileKey is the key used to store the existence of a target in the backend
// (useful when there are no outputs to verify, but we want to keep the directory)
const existsFileKey = "__grog_exists__"

type TargetCache struct {
	backend backends.CacheBackend
}

func NewTargetCache(
	cache backends.CacheBackend,
) *TargetCache {
	return &TargetCache{backend: cache}
}

func (tc *TargetCache) GetBackend() backends.CacheBackend {
	return tc.backend
}

// cachePath returns the path in the backend where the target backend data is stored
// -> {targetPackagePath}/{targetName}_cache_{targetInputHash}
// the key, i.e. {outputHash} or {outputHash}.meta must be supplied separately
func (tc *TargetCache) cachePath(target model.Target) string {
	return fmt.Sprintf(
		"%s/%s_cache_%s",
		target.Label.Package,
		target.Label.Name,
		target.ChangeHash)
}

func (tc *TargetCache) FileExists(ctx context.Context, target model.Target, output model.Output) (bool, error) {
	return tc.backend.Exists(ctx, tc.cachePath(target), hashing.HashString(output.String()))
}

// WriteFile writes a file path relative to the target path to the cache backend
func (tc *TargetCache) WriteFile(ctx context.Context, target model.Target, output model.Output) error {
	absOutputPath := config.GetPathAbsoluteToWorkspaceRoot(filepath.Join(target.Label.Package, output.Identifier))
	outputReader, err := os.Open(absOutputPath)
	if err != nil {
		return fmt.Errorf("declared output %s for target %s was not created", output, target.Label)
	}

	return tc.backend.Set(ctx, tc.cachePath(target), hashing.HashString(output.String()), outputReader)
}

// WriteFileStream same as WriteFile but takes a reader instead of trying to open the file directly
func (tc *TargetCache) WriteFileStream(ctx context.Context, target model.Target, output model.Output, reader io.Reader) error {
	return tc.backend.Set(ctx, tc.cachePath(target), hashing.HashString(output.String()), reader)
}

// LoadFile loads a cached file from the cache backend and writes it to the given path
func (tc *TargetCache) LoadFile(ctx context.Context, target model.Target, output model.Output) error {
	absOutputPath := config.GetPathAbsoluteToWorkspaceRoot(filepath.Join(target.Label.Package, output.Identifier))
	contentReader, err := tc.backend.Get(ctx, tc.cachePath(target), hashing.HashString(output.String()))
	if err != nil {
		return fmt.Errorf("output %s for target %s does not exist in %s backend",
			output.String(),
			target.Label,
			tc.backend.TypeName())
	}

	// TODO should we (re-)store the file permissions as-well somehow?
	outputFile, err := os.Create(absOutputPath)
	if err != nil {
		return err
	}

	if _, err := io.Copy(outputFile, contentReader); err != nil {
		return err
	}

	return outputFile.Close()
}

func (tc *TargetCache) LoadFileStream(ctx context.Context, target model.Target, output model.Output) (io.ReadCloser, error) {
	return tc.backend.Get(ctx, tc.cachePath(target), hashing.HashString(output.String()))
}

// HasCacheExistsFile only checks if the default file we use (for empty outputs) is present
func (tc *TargetCache) HasCacheExistsFile(ctx context.Context, target model.Target) (bool, error) {
	// check that the existsFileKey is present in the backend
	return tc.backend.Exists(ctx, tc.cachePath(target), existsFileKey)
}

// WriteCacheExistsFile write the empty cache exists file to the backend
func (tc *TargetCache) WriteCacheExistsFile(ctx context.Context, target model.Target) error {
	return tc.backend.Set(ctx, tc.cachePath(target), existsFileKey, bytes.NewReader([]byte{}))
}
