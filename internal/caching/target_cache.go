package caching

import (
	"fmt"
	"grog/internal/config"
	"grog/internal/hashing"
	"grog/internal/model"
	"os"
	"path/filepath"
)

// existsFileKey is the key used to store the existence of a target in the cache
// (useful when there are no outputs to verify, but we want to keep the directory)
const existsFileKey = "__grog_exists__"

type TargetCache struct {
	cache Cache
}

func NewTargetCache(
	cache Cache,
) *TargetCache {
	return &TargetCache{cache: cache}
}

func (tc *TargetCache) GetCache() Cache {
	return tc.cache
}

// cachePath returns the path in the cache where the target cache data is stored
// -> {targetPackagePath}/{targetName}_cache_{targetInputHash}
func (tc *TargetCache) cachePath(target model.Target) string {
	return fmt.Sprintf(
		"%s/%s_cache_%s",
		target.Label.Package,
		target.Label.Name,
		target.ChangeHash)
}

// cacheOutputPath returns the path in the cache where a single output is stored
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

func (tc *TargetCache) HasCacheHit(target model.Target) bool {
	// check all specified outputs exist in the cache
	for _, output := range target.Outputs {
		if !tc.cache.Exists(tc.cachePath(target), hashing.HashString(output)) {
			return false
		}
	}

	// check that the existsFileKey is present in the cache
	return tc.cache.Exists(tc.cachePath(target), existsFileKey)
}

// LoadOutputs loads all outputs from the cache and fails if they do not exist
// (existence should be checked before calling this function)
func (tc *TargetCache) LoadOutputs(target model.Target) error {
	for _, output := range target.Outputs {
		// read output from file
		absOutputPath := config.GetPathAbsoluteToWorkspaceRoot(filepath.Join(target.Label.Package, output))
		content, exists := tc.cache.Get(tc.cachePath(target), hashing.HashString(output))
		if !exists {
			return fmt.Errorf("output %s for target %s does not exist in %s cache",
				output,
				target.Label,
				tc.cache.TypeName())
		}

		// TODO should we store the file permissions as-well somehow?
		if err := os.WriteFile(absOutputPath, content, 0644); err != nil {
			return err
		}
	}

	return nil
}

// WriteOutputs Writes a target's outputs to the cache.
func (tc *TargetCache) WriteOutputs(target model.Target) error {
	for _, output := range target.Outputs {
		// read output from file
		absOutputPath := config.GetPathAbsoluteToWorkspaceRoot(filepath.Join(target.Label.Package, output))
		outputContent, err := os.ReadFile(absOutputPath)
		if err != nil {
			return fmt.Errorf("declared output %s for target %s was not created", output, target.Label)
		}
		if err = tc.cache.Set(tc.cachePath(target), hashing.HashString(output), outputContent); err != nil {
			return err
		}
	}

	// write existsFileKey to cache
	return tc.cache.Set(tc.cachePath(target), existsFileKey, []byte{})
}
