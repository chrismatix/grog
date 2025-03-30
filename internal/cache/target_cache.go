package cache

import (
	"fmt"
	"grog/internal/label"
)

// existsFileKey is the key used to store the existence of a target in the cache
// (useful so when there are no outputs to verify)
const existsFileKey = "__grog_exists__"

type TargetCache struct {
	target    label.TargetLabel
	inputHash string
	cache     Cache
}

func NewTargetCache(
	target label.TargetLabel,
	inputHash string,
	cache Cache,
) *TargetCache {
	return &TargetCache{target: target, inputHash: inputHash, cache: cache}
}

// cachePath returns the path in the cache where the target cache data is stored
// -> {targetPackagePath}/{targetName}_cache_{targetInputHash}
func (tc *TargetCache) cachePath() string {
	return fmt.Sprintf(
		"%s/%s_cache_%s",
		tc.target.Package,
		tc.target.Name,
		tc.inputHash)
}

func (tc *TargetCache) Exists(Outputs []string) bool {
	// check all specified outputs exist in the cache
	for _, output := range Outputs {
		if !tc.cache.Exists(tc.cachePath(), output) {
			return false
		}
	}

	return tc.cache.Exists(tc.cachePath(), existsFileKey)
}
