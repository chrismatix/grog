package analysis

import (
	"testing"

	"github.com/stretchr/testify/require"

	"grog/internal/label"
	"grog/internal/model"
	"grog/internal/output"
)

func TestBuildGraphDetectsIndependentOutputConflicts(t *testing.T) {
	firstOutputs, err := output.ParseOutputs([]string{"dist/app.tar"})
	require.NoError(t, err)

	secondOutputs, err := output.ParseOutputs([]string{"dist/app.tar"})
	require.NoError(t, err)

	nodes := model.BuildNodeMap{
		label.TL("", "first"): &model.Target{
			Label:   label.TL("", "first"),
			Outputs: firstOutputs,
		},
		label.TL("", "second"): &model.Target{
			Label:   label.TL("", "second"),
			Outputs: secondOutputs,
		},
	}

	_, buildErr := BuildGraph(nodes)

	require.Error(t, buildErr)
	require.Contains(t, buildErr.Error(), "conflicting outputs detected")
	require.Contains(t, buildErr.Error(), "race condition")
}

func TestBuildGraphAllowsOrderedConflictingOutputs(t *testing.T) {
	firstOutputs, err := output.ParseOutputs([]string{"dist/app.tar"})
	require.NoError(t, err)

	secondOutputs, err := output.ParseOutputs([]string{"dist/app.tar"})
	require.NoError(t, err)

	nodes := model.BuildNodeMap{
		label.TL("", "first"): &model.Target{
			Label:   label.TL("", "first"),
			Outputs: firstOutputs,
		},
		label.TL("", "second"): &model.Target{
			Label:        label.TL("", "second"),
			Outputs:      secondOutputs,
			Dependencies: []label.TargetLabel{label.TL("", "first")},
		},
	}

	graph, buildErr := BuildGraph(nodes)

	require.NoError(t, buildErr)
	require.NotNil(t, graph)
}
