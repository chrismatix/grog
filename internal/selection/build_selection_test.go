package selection

import (
	"grog/internal/config"
	"grog/internal/dag"
	"grog/internal/label"
	"grog/internal/model"
	"strings"
	"testing"
)

func TestSelectTargetsForBuild(t *testing.T) {
	config.Global.OS = "darwin"
	config.Global.Arch = "amd64"

	// Create a target pattern that matches any target in package "pkg"
	pattern, err := label.ParseTargetPattern("pkg", "//pkg:all")
	if err != nil {
		t.Fatalf("failed to parse target pattern: %v", err)
	}
	testTag := "tag1"

	t.Run("selection with platform skipping", func(t *testing.T) {
		graph := dag.NewDirectedGraph()

		// target1 qualifies for selection (platform is nil => match); set Package field.
		target1 := &model.Target{
			Label: label.TargetLabel{
				Name:    "target1",
				Package: "pkg",
			},
			Tags: []string{testTag},
		}
		// target2 qualifies by pattern and tag but has a platform mismatch.
		target2 := &model.Target{
			Label: label.TargetLabel{
				Name:    "target2",
				Package: "pkg",
			},
			Tags: []string{testTag},
			Platform: &model.PlatformConfig{
				OS:   []string{"linux"}, // does not match "darwin"
				Arch: []string{"arm64"},
			},
		}

		graph.AddVertex(target1)
		graph.AddVertex(target2)

		// Create a selector with NonTestOnly filter
		selector := New([]label.TargetPattern{pattern}, []string{testTag}, []string{}, NonTestOnly)

		// Call SelectTargetsForBuild
		selected, skipped, err := selector.SelectTargetsForBuild(graph)
		if err != nil {
			t.Fatalf("SelectTargetsForBuild returned unexpected error: %v", err)
		}
		if selected != 1 {
			t.Errorf("Expected 1 selected target, got %d", selected)
		}
		if skipped != 1 {
			t.Errorf("Expected 1 platform-skipped target, got %d", skipped)
		}
	})

	t.Run("dependency platform mismatch producing an error", func(t *testing.T) {
		graph := dag.NewDirectedGraph()

		// target3 qualifies and is intended to be selected.
		target3 := &model.Target{
			Label: label.TargetLabel{
				Name:    "target3",
				Package: "pkg",
			},
			Tags: []string{testTag},
		}
		// target4 is a dependency with a platform mismatch.
		target4 := &model.Target{
			Label: label.TargetLabel{
				Name:    "target4",
				Package: "pkg",
			},
			Tags: []string{testTag},
			Platform: &model.PlatformConfig{
				OS:   []string{"linux"}, // mismatch
				Arch: []string{"arm64"},
			},
		}

		graph.AddVertex(target3)
		graph.AddVertex(target4)

		// Create a dependency: target3 depends on target4.
		if err := graph.AddEdge(target4, target3); err != nil {
			t.Fatalf("Unexpected error adding edge: %v", err)
		}

		// Create a selector with NonTestOnly filter
		selector := New([]label.TargetPattern{pattern}, []string{testTag}, []string{}, NonTestOnly)

		// Call SelectTargetsForBuild
		_, _, err = selector.SelectTargetsForBuild(graph)
		if err == nil {
			t.Fatal("Expected error due to dependency platform mismatch, but got nil")
		}
		if !strings.Contains(err.Error(), "does not match the platform") {
			t.Errorf("Unexpected error message: %v", err)
		}
	})

	t.Run("filtering by test flag", func(t *testing.T) {
		graph := dag.NewDirectedGraph()

		// target5 is a test target identified by the _test suffix.
		target5 := &model.Target{
			Label: label.TargetLabel{
				Name:    "target5_test",
				Package: "pkg",
			},
			Tags: []string{testTag},
		}

		// target6 is a non-test target.
		target6 := &model.Target{
			Label: label.TargetLabel{
				Name:    "target6",
				Package: "pkg",
			},
			Tags: []string{testTag},
		}

		graph.AddVertex(target5)
		graph.AddVertex(target6)

		// Create a selector with TestOnly filter
		testSelector := New([]label.TargetPattern{pattern}, []string{testTag}, []string{}, TestOnly)

		// When TestFilter is TestOnly, only target5 should be selected.
		selected, skipped, err := testSelector.SelectTargetsForBuild(graph)
		if err != nil {
			t.Fatalf("SelectTargetsForBuild returned unexpected error: %v", err)
		}
		if selected != 1 {
			t.Errorf("Expected 1 selected test target, got %d", selected)
		}
		if skipped != 0 {
			t.Errorf("Expected 0 platform-skipped targets, got %d", skipped)
		}

		// Reset selection flags.
		target5.IsSelected = false
		target6.IsSelected = false

		// Create a selector with NonTestOnly filter
		nonTestSelector := New([]label.TargetPattern{pattern}, []string{testTag}, []string{}, NonTestOnly)

		// When TestFilter is NonTestOnly, only target6 should be selected.
		selected, skipped, err = nonTestSelector.SelectTargetsForBuild(graph)
		if err != nil {
			t.Fatalf("SelectTargetsForBuild returned unexpected error: %v", err)
		}
		if selected != 1 {
			t.Errorf("Expected 1 selected non-test target, got %d", selected)
		}
		if skipped != 0 {
			t.Errorf("Expected 0 platform-skipped targets, got %d", skipped)
		}
	})
	t.Run("filtering by exclude tags", func(t *testing.T) {
		graph := dag.NewDirectedGraph()

		excludeTag := "exclude_me"

		// target7 has only the test tag.
		target7 := &model.Target{
			Label: label.TargetLabel{
				Name:    "target7",
				Package: "pkg",
			},
			Tags: []string{testTag},
		}

		// target8 has both the test tag and the exclude tag.
		target8 := &model.Target{
			Label: label.TargetLabel{
				Name:    "target8",
				Package: "pkg",
			},
			Tags: []string{testTag, excludeTag},
		}

		graph.AddVertex(target7)
		graph.AddVertex(target8)

		// Create a selector with an exclude tag
		selector := New([]label.TargetPattern{pattern}, []string{testTag}, []string{excludeTag}, AllTargets)

		// Only target7 should be selected, target8 should be excluded due to its exclude tag
		selected, skipped, err := selector.SelectTargetsForBuild(graph)
		if err != nil {
			t.Fatalf("SelectTargetsForBuild returned unexpected error: %v", err)
		}
		if selected != 1 {
			t.Errorf("Expected 1 selected target, got %d", selected)
		}
		if skipped != 0 {
			t.Errorf("Expected 0 platform-skipped targets, got %d", skipped)
		}

		// Verify the correct target was selected
		if !target7.IsSelected {
			t.Errorf("Expected target7 to be selected")
		}
		if target8.IsSelected {
			t.Errorf("Expected target8 to be excluded")
		}
	})
}
