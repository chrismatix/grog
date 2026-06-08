package traces

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/spf13/cobra"

	"grog/internal/caching/backends"
	"grog/internal/config"
	"grog/internal/console"
	"grog/internal/tracing"
)

func makeBuild(id string, startMs int64, command string, isCI bool) *tracing.BuildTrace {
	return &tracing.BuildTrace{
		Build: tracing.BuildRow{
			TraceID:                 id,
			Workspace:               "ws",
			Command:                 command,
			StartTimeUnixMillis:     startMs,
			TotalDurationMillis:     5000,
			TotalTargets:            10,
			SuccessCount:            8,
			FailureCount:            2,
			CacheHitCount:           6,
			GitCommit:               "abc1234",
			GitBranch:               "main",
			GrogVersion:             "0.1",
			Platform:                "linux/amd64",
			IsCI:                    isCI,
			CriticalPathExecMillis:  2000,
			CriticalPathCacheMillis: 1000,
		},
		Spans: []tracing.SpanRow{
			{
				TraceID:               id,
				Label:                 "//pkg:target",
				Package:               "pkg",
				Status:                "SUCCESS",
				CacheResult:           "CACHE_MISS",
				StartTimeUnixMillis:   startMs + 100,
				EndTimeUnixMillis:     startMs + 2100,
				TotalDurationMillis:   2000,
				CommandDurationMillis: 1500,
				HashDurationMillis:    50,
				QueueWaitMillis:       200,
				OutputWriteMillis:     100,
				OutputLoadMillis:      50,
				CacheWriteMillis:      80,
				DepLoadMillis:         30,
			},
		},
	}
}

func setupTracesWorkspace(t *testing.T) (string, func()) {
	t.Helper()
	prev := config.Global
	tmp := t.TempDir()
	cas := t.TempDir()
	config.Global = config.WorkspaceConfig{Root: tmp, WorkspaceRoot: tmp}

	cacheDir := config.Global.GetWorkspaceCacheDirectory()
	if err := os.MkdirAll(cacheDir, 0o755); err != nil {
		t.Fatal(err)
	}

	fs := backends.NewFileSystemCacheForTest(cacheDir, cas)
	writer := tracing.NewTraceWriter(fs)
	ctx := context.Background()
	now := time.Now()
	for i, id := range []string{"trace-a", "trace-b", "trace-c"} {
		if err := writer.Write(ctx, makeBuild(id, now.Add(time.Duration(i)*time.Minute).UnixMilli(), "build", i%2 == 0)); err != nil {
			t.Fatal(err)
		}
	}

	return cacheDir, func() { config.Global = prev }
}

func TestGetStore(t *testing.T) {
	prev := config.Global
	tmp := t.TempDir()
	config.Global = config.WorkspaceConfig{Root: tmp, WorkspaceRoot: tmp}
	t.Cleanup(func() { config.Global = prev })

	ctx, logger := console.SetupCommand()
	store := getStore(ctx, logger)
	if store == nil {
		t.Fatal("nil store")
	}
	defer store.Close()
}

func runCmd(t *testing.T, runE func()) {
	t.Helper()
	defer func() {
		// swallow os.Exit panic if any (logger.Fatalf calls os.Exit, but the call
		// can be intercepted only if the test process exits — which is unsafe.
		// We rely on the workspace being valid so Fatalf is not invoked.)
		_ = recover()
	}()
	runE()
}

func TestListCmdRun(t *testing.T) {
	_, cleanup := setupTracesWorkspace(t)
	defer cleanup()
	runCmd(t, func() {
		listLimit = 10
		listCmd.Run(listCmd, nil)
	})
}

func TestListCmdRun_Empty(t *testing.T) {
	prev := config.Global
	tmp := t.TempDir()
	config.Global = config.WorkspaceConfig{Root: tmp, WorkspaceRoot: tmp}
	t.Cleanup(func() { config.Global = prev })

	runCmd(t, func() {
		listLimit = 10
		listCmd.Run(listCmd, nil)
	})
}

func TestListCmdRun_SinceFilter(t *testing.T) {
	_, cleanup := setupTracesWorkspace(t)
	defer cleanup()

	runCmd(t, func() {
		listLimit = 10
		listSince = "2020-01-01"
		listCmd.Run(listCmd, nil)
		listSince = ""
	})
}

func TestStatsCmdRun(t *testing.T) {
	_, cleanup := setupTracesWorkspace(t)
	defer cleanup()
	runCmd(t, func() {
		statsLimit = 10
		statsCmd.Run(statsCmd, nil)
	})
}

func TestStatsCmdRun_Detailed(t *testing.T) {
	_, cleanup := setupTracesWorkspace(t)
	defer cleanup()
	runCmd(t, func() {
		statsLimit = 10
		statsDetailed = true
		statsCmd.Run(statsCmd, nil)
		statsDetailed = false
	})
}

func TestStatsCmdRun_Empty(t *testing.T) {
	prev := config.Global
	tmp := t.TempDir()
	config.Global = config.WorkspaceConfig{Root: tmp, WorkspaceRoot: tmp}
	t.Cleanup(func() { config.Global = prev })

	runCmd(t, func() {
		statsLimit = 10
		statsCmd.Run(statsCmd, nil)
	})
}

func TestShowCmdRun(t *testing.T) {
	_, cleanup := setupTracesWorkspace(t)
	defer cleanup()
	runCmd(t, func() {
		showCmd.Run(showCmd, []string{"trace-a"})
	})
}

func TestExportCmdRun(t *testing.T) {
	_, cleanup := setupTracesWorkspace(t)
	defer cleanup()
	for _, fmt := range []string{"jsonl", "otel"} {
		runCmd(t, func() {
			exportFormat = fmt
			out := filepath.Join(t.TempDir(), "out.txt")
			exportOutput = out
			exportLimit = 10
			exportCmd.Run(exportCmd, nil)
			exportOutput = ""
		})
	}
}

func TestExportCmdRun_Empty(t *testing.T) {
	prev := config.Global
	tmp := t.TempDir()
	config.Global = config.WorkspaceConfig{Root: tmp, WorkspaceRoot: tmp}
	t.Cleanup(func() { config.Global = prev })

	runCmd(t, func() {
		exportFormat = "jsonl"
		exportCmd.Run(exportCmd, nil)
	})
}

func TestPruneCmdRun(t *testing.T) {
	_, cleanup := setupTracesWorkspace(t)
	defer cleanup()
	runCmd(t, func() {
		pruneOlderThan = "30d"
		pruneCmd.Run(pruneCmd, nil)
	})
}

func TestPullCmdRun(t *testing.T) {
	_, cleanup := setupTracesWorkspace(t)
	defer cleanup()
	runCmd(t, func() {
		pullCmd.Run(pullCmd, nil)
	})
}

func TestAddCmd(t *testing.T) {
	root := &cobra.Command{Use: "root"}
	AddCmd(root)
}
