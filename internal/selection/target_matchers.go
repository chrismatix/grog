package selection

import (
	"grog/internal/config"
	"grog/internal/model"
	"slices"
)

func targetMatchesPlatform(target *model.Target) bool {
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

func targetMatchesTestSelection(target *model.Target, testMode TestSelection) bool {
	switch testMode {
	case TestOnly:
		return target.IsTest()
	case NonTestOnly:
		return !target.IsTest()
	case AllTargets:
		return true
	}

	return false
}
