package main

import (
	"grog/internal/cmd/cmds"
	"grog/internal/config"
	"grog/internal/label"
	"grog/internal/loading"
	"grog/internal/selection"
	"testing"

	"go.uber.org/zap/zaptest"
)

func TestProfilingBuildIntegrated(t *testing.T) {

	t.Run("test", func(t *testing.T) {
		repoPath := "/Users/christophproschel/codingprojects/binit/core" // materializeProfilingRepo(t, definition)

		callBuildFunction(t, repoPath)

		t.Logf("Profiling complete")
	})
}

func callBuildFunction(t *testing.T, repoPath string) {
	testLogger := zaptest.NewLogger(t).Sugar()

	config.Global.Root = "/Users/christophproschel/.grog"
	config.Global.WorkspaceRoot = repoPath
	graph := loading.MustLoadGraphForBuild(t.Context(), testLogger)

	cmds.RunBuild(
		t.Context(),
		testLogger,
		[]label.TargetPattern{label.TargetPatternFromLabel(label.TL("backend/api", "pex"))},
		graph,
		selection.NonTestOnly,
		config.Global.StreamLogs,
		config.Global.GetLoadOutputsMode(),
	)
}
