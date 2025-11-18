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

// taintedFileKey is the key used to mark a target as tainted
// (forcing it to execute regardless of cache status)
const taintedFileKey = "__grog_tainted"

const outputDigest = "_digest"

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

// CachePath returns the path in the backend where the target backend data is stored
// -> {targetPackagePath}/{targetName}_cache_{targetInputHash}
// the key, i.e. {outputHash} or {outputHash}.meta must be supplied separately
// TODO this should fail if the change hash is empty
func (tc *TargetCache) CachePath(target model.Target) string {
	return fmt.Sprintf(
		"%s/%s_cache_%s",
		target.Label.Package,
		target.Label.Name,
		target.ChangeHash)
}

func (tc *TargetCache) CacheKey(output model.Output) string {
	return hashing.HashString(output.String())
}

func (tc *TargetCache) MetaCacheKey(output model.Output, metaKey string) string {
	return fmt.Sprintf("%s_%s", tc.CacheKey(output), metaKey)
}

func (tc *TargetCache) FileExists(ctx context.Context, target model.Target, output model.Output) (bool, error) {
	return tc.backend.Exists(ctx, tc.CachePath(target), tc.CacheKey(output))
}

// WriteOutputMetaFile writes a meta key/value for a given output
// Output handlers can use this to track the contents of an output and thereby decide when to load it
// Note: Meta values are usually small and thus should not require streaming
func (tc *TargetCache) WriteOutputMetaFile(
	ctx context.Context,
	target model.Target,
	output model.Output,
	metaKey string,
	metaValue string,
) error {
	return tc.backend.Set(ctx, tc.CachePath(target), tc.MetaCacheKey(output, metaKey), bytes.NewReader([]byte(metaValue)))
}

// WriteOutputDigest writes a digest for a given at a fixed key
func (tc *TargetCache) WriteOutputDigest(
	ctx context.Context,
	target model.Target,
	output model.Output,
	metaValue string,
) error {
	return tc.WriteOutputMetaFile(ctx, target, output, outputDigest, metaValue)
}

func (tc *TargetCache) LoadOutputMetaFile(
	ctx context.Context,
	target model.Target,
	output model.Output,
	metaKey string,
) (string, error) {
	contentReader, err := tc.backend.Get(ctx, tc.CachePath(target), tc.MetaCacheKey(output, metaKey))
	if err != nil {
		return "", fmt.Errorf("output %s for target %s does not exist in %s backend",
			output.String(),
			target.Label,
			tc.backend.TypeName())
	}
	defer contentReader.Close()
	buf := new(bytes.Buffer)
	if _, err := io.Copy(buf, contentReader); err != nil {
		return "", err
	}
	return buf.String(), nil
}

func (tc *TargetCache) LoadOutputDigest(ctx context.Context, target model.Target, output model.Output) (string, error) {
	return tc.LoadOutputMetaFile(ctx, target, output, outputDigest)
}

func (tc *TargetCache) HasOutputMetaFile(
	ctx context.Context,
	target model.Target,
	output model.Output,
	metaKey string,
) (bool, error) {
	return tc.backend.Exists(ctx, tc.CachePath(target), tc.MetaCacheKey(output, metaKey))
}

// WriteFile writes a file path relative to the target path to the cache backend
func (tc *TargetCache) WriteFile(ctx context.Context, target model.Target, output model.Output) error {
	absOutputPath := config.GetPathAbsoluteToWorkspaceRoot(filepath.Join(target.Label.Package, output.Identifier))
	outputReader, err := os.Open(absOutputPath)
	if err != nil {
		return fmt.Errorf("declared output %s for target %s was not created", output, target.Label)
	}

	return tc.backend.Set(ctx, tc.CachePath(target), tc.CacheKey(output), outputReader)
}

// WriteFileStream same as WriteFile but takes a reader instead of trying to open the file directly
func (tc *TargetCache) WriteFileStream(ctx context.Context, target model.Target, output model.Output, reader io.Reader) error {
	return tc.backend.Set(ctx, tc.CachePath(target), tc.CacheKey(output), reader)
}

// LoadFile loads a cached file from the cache backend and writes it to the given path
func (tc *TargetCache) LoadFile(ctx context.Context, target model.Target, output model.Output) error {
	absOutputPath := config.GetPathAbsoluteToWorkspaceRoot(filepath.Join(target.Label.Package, output.Identifier))
	contentReader, err := tc.backend.Get(ctx, tc.CachePath(target), tc.CacheKey(output))
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
	return tc.backend.Get(ctx, tc.CachePath(target), tc.CacheKey(output))
}

// HasCacheExistsFile only checks if the default file we use (for empty outputs) is present
func (tc *TargetCache) HasCacheExistsFile(ctx context.Context, target model.Target) (bool, error) {
	// check that the existsFileKey is present in the backend
	return tc.backend.Exists(ctx, tc.CachePath(target), existsFileKey)
}

// WriteCacheExistsFile write the empty cache exists file to the backend
func (tc *TargetCache) WriteCacheExistsFile(ctx context.Context, target model.Target) error {
	return tc.backend.Set(ctx, tc.CachePath(target), existsFileKey, bytes.NewReader([]byte{}))
}

// WriteLocalCacheExistsFile write the empty cache exists file to the local cache only
// Context: If we restored the cache from remote we want to mark the local cache with the existsFileKey
// However, the registry calls .Load() per handler
func (tc *TargetCache) WriteLocalCacheExistsFile(ctx context.Context, target model.Target) error {
	if fsCache, ok := tc.backend.(*backends.FileSystemCache); ok {
		return fsCache.Set(ctx, tc.CachePath(target), existsFileKey, bytes.NewReader([]byte{}))
	} else if remoteWrapper, ok := tc.backend.(*backends.RemoteWrapper); ok {
		return remoteWrapper.GetFS().Set(ctx, tc.CachePath(target), existsFileKey, bytes.NewReader([]byte{}))
	}

	// Default to just writing to whatever cache we have
	return tc.backend.Set(ctx, tc.CachePath(target), existsFileKey, bytes.NewReader([]byte{}))
}

// Taint marks a target as tainted, forcing it to execute regardless of cache status
func (tc *TargetCache) Taint(ctx context.Context, target model.Target) error {
	return tc.backend.Set(ctx, tc.CachePath(target), taintedFileKey, bytes.NewReader([]byte{}))
}

// IsTainted checks if a target is tainted
func (tc *TargetCache) IsTainted(ctx context.Context, target model.Target) (bool, error) {
	return tc.backend.Exists(ctx, tc.CachePath(target), taintedFileKey)
}

// RemoveTaint removes the taint from a target
func (tc *TargetCache) RemoveTaint(ctx context.Context, target model.Target) error {
	return tc.backend.Delete(ctx, tc.CachePath(target), taintedFileKey)
}
