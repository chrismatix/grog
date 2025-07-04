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

	if target.Platform == nil {
		return true
	}

	if len(target.Platform.OS) != 0 && !slices.Contains(target.Platform.OS, config.Global.OS) {
		return false
	}
	if len(target.Platform.Arch) != 0 && !slices.Contains(target.Platform.Arch, config.Global.Arch) {
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
