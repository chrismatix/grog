package selection

import (
	"fmt"
	"grog/internal/label"
	"grog/internal/model"
	"slices"
)

type TargetTypeSelection string

const (
	TestOnly    TargetTypeSelection = "test_only"
	NonTestOnly TargetTypeSelection = "non_test_only"
	BinOutput   TargetTypeSelection = "bin_output"
	AllTargets  TargetTypeSelection = "all_targets"
)

// StringToTargetTypeSelection converts a string to a TargetTypeSelection
func StringToTargetTypeSelection(targetType string) (TargetTypeSelection, error) {
	switch targetType {
	case "test":
		return TestOnly, nil
	case "no_test":
		return NonTestOnly, nil
	case "bin_output":
		return BinOutput, nil
	case "all":
		return AllTargets, nil
	default:
		return AllTargets, fmt.Errorf("invalid target type: %s", targetType)
	}
}

// Selector bundles the default selection options into a single interface
type Selector struct {
	Patterns    []label.TargetPattern
	Tags        []string
	ExcludeTags []string
	TargetType  TargetTypeSelection
}

func New(
	patterns []label.TargetPattern,
	tags []string,
	excludeTags []string,
	targetType TargetTypeSelection,
) *Selector {
	return &Selector{Patterns: patterns, Tags: tags, ExcludeTags: excludeTags, TargetType: targetType}
}

func (s *Selector) targetMatchesFilters(
	target *model.Target,
) bool {
	return s.targetMatchesTypeSelection(target) &&
		s.targetMatchesPatterns(target) &&
		s.targetTagsMatch(target) &&
		!s.targetExcludeTagsMatch(target)
}

func (s *Selector) targetMatchesPatterns(target *model.Target) bool {
	for _, pattern := range s.Patterns {
		if pattern.Matches(target.Label) {
			return true
		}
	}
	return len(s.Patterns) == 0
}

func (s *Selector) targetTagsMatch(target *model.Target) bool {
	hasTag := false
	for _, tag := range s.Tags {
		if slices.Contains(target.Tags, tag) {
			hasTag = true
			break
		}
	}

	return hasTag || len(s.Tags) == 0
}

func (s *Selector) targetExcludeTagsMatch(target *model.Target) bool {
	hasTag := false
	for _, tag := range s.ExcludeTags {
		if slices.Contains(target.Tags, tag) {
			hasTag = true
			break
		}
	}

	return hasTag
}

func (s *Selector) targetMatchesTypeSelection(target *model.Target) bool {
	return TargetMatchesTypeSelection(target, s.TargetType)
}
