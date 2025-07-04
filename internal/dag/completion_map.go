package dag

import (
	"grog/internal/label"
)

type CompletionMap map[label.TargetLabel]Completion

func (c CompletionMap) GetErrors() []error {
	var errorList []error
	for _, completion := range c {
		if !completion.IsSuccess {
			errorList = append(errorList, completion.Err)
		}
	}
	return errorList
}

// SuccessCount returns the number of successful targets and the number of cache hits
func (c CompletionMap) SuccessCount() (int, int) {
	successCount := 0
	cacheHits := 0
	for _, completion := range c {
		if completion.IsSuccess {
			successCount++
			if completion.CacheResult == CacheHit {
				cacheHits++
			}
		}
	}
	return successCount, cacheHits
}
