package selection

import (
	"grog/internal/config"
	"grog/internal/model"
	"slices"
)

func nodeMatchesPlatform(node model.BuildNode) bool {
	target, ok := node.(*model.Target)
	if !ok {
		return true
	}

	if config.Global.AllPlatforms {
		return true
	}

	if len(target.Platforms) == 0 {
		return true
	}

	if !slices.Contains(target.Platforms, config.Global.GetPlatform()) {
		return false
	}

	return true
}

func TargetMatchesTypeSelection(target *model.Target, targetType TargetTypeSelection) bool {
	switch targetType {
	case TestOnly:
		return target.IsTest()
	case NonTestOnly:
		return !target.IsTest()
	case BinOutput:
		return target.HasBinOutput()
	case AllTargets:
		return true
	}

	return false
}
