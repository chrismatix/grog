package selection

import (
	"grog/internal/label"
)

type TestSelection string

const (
	TestOnly    TestSelection = "test_only"
	NonTestOnly TestSelection = "non_test_only"
	AllTargets  TestSelection = "all_targets"
)

type Selector struct {
	Patterns   []label.TargetPattern
	Tags       []string
	TestFilter TestSelection
}

func New(pats []label.TargetPattern, tags []string, testFilter TestSelection) *Selector {
	return &Selector{Patterns: pats, Tags: tags, TestFilter: testFilter}
}
