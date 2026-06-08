package traces

import (
	"testing"

	"grog/internal/tracing"
)

func withStyled(t *testing.T, v bool) {
	t.Helper()
	prev := styled
	styled = func() bool { return v }
	t.Cleanup(func() { styled = prev })
}

func TestRenderHelpers_Styled(t *testing.T) {
	withStyled(t, true)

	if renderSection("hi") == "" {
		t.Fatal("section")
	}
	if renderHint("hi") == "" {
		t.Fatal("hint")
	}
	if renderLabel("hi") == "" {
		t.Fatal("label")
	}
	if renderDim("hi") == "" {
		t.Fatal("dim")
	}
	// renderImpact tiers
	_ = renderImpact(100)   // low
	_ = renderImpact(5000)  // medium
	_ = renderImpact(20000) // high
}

func TestPrintBuildSummary_Styled(t *testing.T) {
	withStyled(t, true)
	b := &tracing.BuildRow{
		TraceID:                 "t1",
		Command:                 "build",
		GrogVersion:             "1.2.3",
		Platform:                "linux/amd64",
		GitCommit:               "abc1234",
		GitBranch:               "main",
		StartTimeUnixMillis:     1700000000000,
		TotalDurationMillis:     2500,
		TotalTargets:            10,
		CacheHitCount:           6,
		FailureCount:            1,
		CriticalPathExecMillis:  1500,
		CriticalPathCacheMillis: 200,
		RequestedPatterns:       "//a,//b",
	}
	printBuildSummary(b)

	// Same with no failures and no critical path.
	b.FailureCount = 0
	b.CriticalPathExecMillis = 0
	b.CriticalPathCacheMillis = 0
	b.RequestedPatterns = ""
	printBuildSummary(b)
}

func TestPrintSpanTable_Styled(t *testing.T) {
	withStyled(t, true)
	spans := []tracing.SpanRow{
		{Label: "//x:a", Status: "SUCCESS", CacheResult: "CACHE_HIT", TotalDurationMillis: 100, CommandDurationMillis: 80},
		{Label: "//x:b", Status: "FAILURE", CacheResult: "CACHE_MISS", TotalDurationMillis: 50},
		{Label: "//x:c", Status: "CANCELLED", CacheResult: "CACHE_SKIP"},
	}
	prev := showTop
	t.Cleanup(func() { showTop = prev })
	showTop = 2
	printSpanTable(spans)
	showTop = 0
	printSpanTable(spans)
}

func TestPrintStatsSummary_Styled(t *testing.T) {
	withStyled(t, true)
	printStatsSummary(&tracing.TraceStats{TraceCount: 5, AvgDuration: 1500, CacheHitRate: 80, TotalFails: 0})
	printStatsSummary(&tracing.TraceStats{TraceCount: 5, AvgDuration: 1500, CacheHitRate: 50, TotalFails: 1})
	printStatsSummary(&tracing.TraceStats{TraceCount: 5, AvgDuration: 1500, CacheHitRate: 10, TotalFails: 3})
}

func TestPrintBottleneckReport_Styled(t *testing.T) {
	withStyled(t, true)
	rep := &tracing.BottleneckReport{
		SlowestTargets: []tracing.TargetBottleneck{
			{Label: "//x:a", Impact: 5000, AvgCmd: 4000, Frequency: 0.8, Count: 4},
		},
		QueueSaturated: []tracing.TargetBottleneck{
			{Label: "//x:q", AvgQueue: 700, Count: 3},
		},
		IOBottlenecks: []tracing.TargetBottleneck{
			{Label: "//x:io", AvgIO: 1200, AvgOutputWrite: 200, AvgOutputLoad: 100, AvgCacheWrite: 900, Count: 2},
		},
		SlowHashing: []tracing.TargetBottleneck{
			{Label: "//x:h", AvgHash: 300, Count: 5},
		},
		FrequentMisses: []tracing.TargetBottleneck{
			{Label: "//x:m", MissRate: 80, Count: 4},
		},
		FlakyTargets: []tracing.TargetBottleneck{
			{Label: "//x:f", Failures: 2, Count: 5},
		},
		OverallCacheMissRate: 30,
	}
	printBottleneckReport(rep)
}

func TestListCmdRun_Styled(t *testing.T) {
	withStyled(t, true)
	_, cleanup := setupTracesWorkspace(t)
	defer cleanup()
	runCmd(t, func() {
		listLimit = 10
		listCmd.Run(listCmd, nil)
	})
}

func TestShowCmdRun_Styled(t *testing.T) {
	withStyled(t, true)
	_, cleanup := setupTracesWorkspace(t)
	defer cleanup()
	runCmd(t, func() {
		showCmd.Run(showCmd, []string{"trace-a"})
	})
}
