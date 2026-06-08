package traces

import (
	"strings"
	"testing"
	"time"

	"grog/internal/tracing"
)

func TestRenderImpact(t *testing.T) {
	if !strings.Contains(renderImpact(100), "100ms") {
		t.Fatal("low")
	}
	if !strings.Contains(renderImpact(4000), "4000ms") {
		t.Fatal("medium")
	}
	if !strings.Contains(renderImpact(15000), "15000ms") {
		t.Fatal("high")
	}
}

func TestRenderHelpers(t *testing.T) {
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
}

func TestFormatDuration(t *testing.T) {
	cases := []struct {
		d    time.Duration
		want string
	}{
		{500 * time.Millisecond, "500ms"},
		{2500 * time.Millisecond, "2.5s"},
	}
	for _, c := range cases {
		if got := formatDuration(c.d); got != c.want {
			t.Fatalf("d=%v got %q", c.d, got)
		}
	}
}

func TestFormatMillis(t *testing.T) {
	if formatMillis(250) != "250ms" {
		t.Fatal("ms")
	}
	if formatMillis(2500) != "2.5s" {
		t.Fatal("s")
	}
}

func TestNormalizeCommand(t *testing.T) {
	for _, in := range []string{"", "build", "test", "run"} {
		if _, err := normalizeCommand(in); err != nil {
			t.Fatalf("in=%q: %v", in, err)
		}
	}
	if _, err := normalizeCommand("nope"); err == nil {
		t.Fatal("expected err")
	}
}

func TestNormalizeStatsCommandType(t *testing.T) {
	for _, in := range []string{"", "all", "build", "test"} {
		if _, err := normalizeStatsCommandType(in); err != nil {
			t.Fatalf("in=%q: %v", in, err)
		}
	}
	if _, err := normalizeStatsCommandType("weird"); err == nil {
		t.Fatal("expected err")
	}
}

func TestNormalizeStatsCI(t *testing.T) {
	v, err := normalizeStatsCI("")
	if err != nil || v != nil {
		t.Fatal("empty")
	}
	v, err = normalizeStatsCI("true")
	if err != nil || v == nil || *v != true {
		t.Fatal("true")
	}
	v, err = normalizeStatsCI("false")
	if err != nil || v == nil || *v != false {
		t.Fatal("false")
	}
	if _, err := normalizeStatsCI("maybe"); err == nil {
		t.Fatal("expected err")
	}
}

func TestPrintStatsSummary(t *testing.T) {
	printStatsSummary(&tracing.TraceStats{
		TraceCount:   5,
		AvgDuration:  1500,
		CacheHitRate: 80,
		TotalFails:   0,
	})
	printStatsSummary(&tracing.TraceStats{
		TraceCount:   5,
		AvgDuration:  1500,
		CacheHitRate: 50,
		TotalFails:   1,
	})
	printStatsSummary(&tracing.TraceStats{
		TraceCount:   5,
		AvgDuration:  1500,
		CacheHitRate: 10,
		TotalFails:   3,
	})
}

func TestPrintBottleneckReport(t *testing.T) {
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
	printBottleneckReport(&tracing.BottleneckReport{})
}

func TestStyled(t *testing.T) {
	_ = styled()
}
