package cache

import (
	"fmt"
	"grog/internal/model"
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

// cachePath returns the path in the cache where the target cache data is stored
// -> {targetPackagePath}/{targetName}_cache_{targetInputHash}
func (tc *TargetCache) cachePath(target model.Target) string {
	return fmt.Sprintf(
		"%s/%s_cache_%s",
		target.Label.Package,
		target.Label.Name,
		target.ChangeHash)
}

func (tc *TargetCache) HashCacheHit(target model.Target) bool {
	// check all specified outputs exist in the cache
	for _, output := range target.Outputs {
		if !tc.cache.Exists(tc.cachePath(target), output) {
			return false
		}
	}

	// check that the existsFileKey is present in the cache
	return tc.cache.Exists(tc.cachePath(target), existsFileKey)
}
