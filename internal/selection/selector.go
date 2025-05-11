package selection

import (
	"grog/internal/label"
	"grog/internal/model"
	"slices"
)

type TestSelection string

const (
	TestOnly    TestSelection = "test_only"
	NonTestOnly TestSelection = "non_test_only"
	AllTargets  TestSelection = "all_targets"
)

type Selector struct {
	Patterns    []label.TargetPattern
	Tags        []string
	ExcludeTags []string
	TestFilter  TestSelection
}

func New(
	patterns []label.TargetPattern,
	tags []string,
	excludeTags []string,
	testFilter TestSelection,
) *Selector {
	return &Selector{Patterns: patterns, Tags: tags, ExcludeTags: excludeTags, TestFilter: testFilter}
}

func (s *Selector) targetMatchesFilters(
	target *model.Target,
) bool {
	return s.targetMatchesPatterns(target) &&
		s.targetMatchesTestSelection(target) &&
		s.targetTagsMatch(target) &&
		!s.targetExcludeTagsMatch(target)
}

func (s *Selector) targetMatchesPatterns(target *model.Target) bool {
	for _, pattern := range s.Patterns {
		if pattern.Matches(target.Label) {
			return true
		}
	}
	return false
}

func (s *Selector) targetMatchesTestSelection(target *model.Target) bool {
	switch s.TestFilter {
	case TestOnly:
		return target.IsTest()
	case NonTestOnly:
		return !target.IsTest()
	case AllTargets:
		return true
	}

	return false
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
	for _, tag := range s.ExcludeTags {
		if slices.Contains(target.Tags, tag) {
			return true // Target has an excluded tag
		}
	}

	return false // Target doesn't have any excluded tags
}
