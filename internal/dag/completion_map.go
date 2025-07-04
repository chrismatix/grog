package dag

import (
	"grog/internal/label"
	"grog/internal/model"
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

// TargetSuccessCount returns the number of successful targets and the number of cache hits
func (c CompletionMap) TargetSuccessCount() (int, int) {
	successCount := 0
	cacheHits := 0
	for _, completion := range c {
		if completion.IsSuccess && completion.NodeType == model.TargetNode {
			successCount++
			if completion.CacheResult == CacheHit {
				cacheHits++
			}
		}
	}
	return successCount, cacheHits
}
