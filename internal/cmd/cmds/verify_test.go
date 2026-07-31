package cmds

import (
	"path/filepath"
	"slices"
	"testing"

	"grog/internal/config"
	"grog/internal/dag"
	"grog/internal/label"
	"grog/internal/model"
)

func TestFindTargetsAffectedByFiles(t *testing.T) {
	workspaceRoot := t.TempDir()
	previousWorkspaceRoot := config.Global.WorkspaceRoot
	config.Global.WorkspaceRoot = workspaceRoot
	t.Cleanup(func() {
		config.Global.WorkspaceRoot = previousWorkspaceRoot
	})

	libraryTarget := &model.Target{
		Label:          label.TargetLabel{Package: "library", Name: "build"},
		Inputs:         []string{"source.go"},
		SourceFilePath: filepath.Join(workspaceRoot, "library", "BUILD.yaml"),
	}
	testTarget := &model.Target{
		Label:        label.TargetLabel{Package: "app", Name: "test"},
		Dependencies: []label.TargetLabel{libraryTarget.Label},
		Tags:         []string{model.TagTestOnly},
	}
	unrelatedTarget := &model.Target{
		Label:  label.TargetLabel{Package: "other", Name: "build"},
		Inputs: []string{"source.go"},
	}

	graph := dag.NewDirectedGraphFromTargets(libraryTarget, testTarget, unrelatedTarget)
	graph.AddEdge(libraryTarget, testTarget)

	testCases := []struct {
		name              string
		changedFiles      []string
		includeDependents bool
		expectedLabels    []string
	}{
		{
			name:         "matches direct input",
			changedFiles: []string{filepath.Join(workspaceRoot, "library", "source.go")},
			expectedLabels: []string{
				"//library:build",
			},
		},
		{
			name:              "includes transitive dependents",
			changedFiles:      []string{filepath.Join(workspaceRoot, "library", "source.go")},
			includeDependents: true,
			expectedLabels: []string{
				"//app:test",
				"//library:build",
			},
		},
		{
			name:         "matches package definition",
			changedFiles: []string{filepath.Join(workspaceRoot, "library", "BUILD.yaml")},
			expectedLabels: []string{
				"//library:build",
			},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			targets := findTargetsAffectedByFiles(graph, testCase.changedFiles, testCase.includeDependents)
			labels := make([]string, 0, len(targets))
			for _, target := range targets {
				labels = append(labels, target.Label.String())
			}
			slices.Sort(labels)
			if !slices.Equal(labels, testCase.expectedLabels) {
				t.Errorf("expected labels %v, got %v", testCase.expectedLabels, labels)
			}
		})
	}
}
