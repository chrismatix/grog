package traces

import (
	"testing"

	"grog/internal/tracing"
)

func TestSortSpans(t *testing.T) {
	for _, sortBy := range []string{"command", "queue", "hash", "total", "weird"} {
		spans := []tracing.SpanRow{
			{Label: "//x:a", TotalDurationMillis: 100, CommandDurationMillis: 80, QueueWaitMillis: 10, HashDurationMillis: 5},
			{Label: "//x:b", TotalDurationMillis: 50, CommandDurationMillis: 40, QueueWaitMillis: 20, HashDurationMillis: 15},
		}
		sortSpans(spans, sortBy)
	}
}

func TestPrintBuildSummary(t *testing.T) {
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
	b.FailureCount = 0
	b.CriticalPathExecMillis = 0
	b.CriticalPathCacheMillis = 0
	b.RequestedPatterns = ""
	printBuildSummary(b)
}

func TestPrintSpanTable(t *testing.T) {
	printSpanTable(nil)
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
