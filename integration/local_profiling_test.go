package main

import (
	"grog/internal/cmd/cmds"
	"grog/internal/config"
	"grog/internal/console"
	"grog/internal/label"
	"grog/internal/loading"
	"grog/internal/selection"
	"testing"

	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest"
)

func TestProfilingBuildIntegrated(t *testing.T) {
	t.Skip()

	t.Run("test", func(t *testing.T) {
		// TODO supply your local repo path
		repoPath := ""

		callBuildFunction(t, repoPath)

		t.Logf("Profiling complete")
	})
}

func callBuildFunction(t *testing.T, repoPath string) {
	testLogger := console.NewFromSugared(zaptest.NewLogger(t).Sugar(), zapcore.DebugLevel)

	// TODO supply your local cache root
	config.Global.Root = ""
	config.Global.WorkspaceRoot = repoPath
	config.Global.EnableCache = true
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
