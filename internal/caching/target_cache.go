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
func (tc *TargetCache) cachePath(target model.Target) string {
	return fmt.Sprintf(
		"%s/%s_cache_%s",
		target.Label.Package,
		target.Label.Name,
		target.ChangeHash)
}

// cacheOutputPath returns the path in the backend where a single output is stored
// -> {targetPackagePath}/{targetName}_cache_{targetInputHash}/{outputHash}
func (tc *TargetCache) cacheOutputPath(target model.Target, output string) string {
	return fmt.Sprintf(
		"%s/%s_cache_%s/%s",
		target.Label.Package,
		target.Label.Name,
		target.ChangeHash,
		hashing.HashString(output),
	)
}

func (tc *TargetCache) HasCacheHit(ctx context.Context, target model.Target) bool {
	// check all specified outputs exist in the backend
	for _, output := range target.Outputs {
		if !tc.backend.Exists(ctx, tc.cachePath(target), hashing.HashString(output)) {
			return false
		}
	}

	// check that the existsFileKey is present in the backend
	return tc.backend.Exists(ctx, tc.cachePath(target), existsFileKey)
}

// LoadOutputs loads all outputs from the backend and fails if they do not exist
// (existence should be checked before calling this function)
func (tc *TargetCache) LoadOutputs(ctx context.Context, target model.Target) error {
	for _, output := range target.Outputs {
		// read output from file
		absOutputPath := config.GetPathAbsoluteToWorkspaceRoot(filepath.Join(target.Label.Package, output))
		contentReader, err := tc.backend.Get(ctx, tc.cachePath(target), hashing.HashString(output))
		if err != nil {
			return fmt.Errorf("output %s for target %s does not exist in %s backend",
				output,
				target.Label,
				tc.backend.TypeName())
		}

		// TODO should we store the file permissions as-well somehow?
		outputFile, err := os.Create(absOutputPath)
		if err != nil {
			return err
		}

		if _, err := io.Copy(outputFile, contentReader); err != nil {
			return err
		}

		err = outputFile.Close()
		if err != nil {
			return err
		}
	}

	return nil
}

// WriteOutputs Writes a target's outputs to the backend.
func (tc *TargetCache) WriteOutputs(ctx context.Context, target model.Target) error {
	for _, output := range target.Outputs {
		// read output from file
		absOutputPath := config.GetPathAbsoluteToWorkspaceRoot(filepath.Join(target.Label.Package, output))
		outputReader, err := os.Open(absOutputPath)
		if err != nil {
			return fmt.Errorf("declared output %s for target %s was not created", output, target.Label)
		}
		if err = tc.backend.Set(ctx, tc.cachePath(target), hashing.HashString(output), outputReader); err != nil {
			return err
		}
	}

	// write existsFileKey to backend
	return tc.backend.Set(ctx, tc.cachePath(target), existsFileKey, bytes.NewReader([]byte{}))
}
