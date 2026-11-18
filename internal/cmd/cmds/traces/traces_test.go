package traces

import (
	"bytes"
	"io"
	"os"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/creack/pty"

	"github.com/chrismatix/grog/internal/tracing"
)

// captureOutput runs function with os.Stdout redirected and returns what it printed.
// With terminal set, stdout is a pseudo-terminal so that styled() reports true.
func captureOutput(t *testing.T, terminal bool, function func()) string {
	t.Helper()
	var reader, writer *os.File
	var err error
	if terminal {
		reader, writer, err = pty.Open()
	} else {
		reader, writer, err = os.Pipe()
	}
	if err != nil {
		t.Fatalf("failed to open output: %v", err)
	}
	originalStdout := os.Stdout
	os.Stdout = writer
	done := make(chan string)
	go func() {
		var buffer bytes.Buffer
		_, _ = io.Copy(&buffer, reader)
		done <- buffer.String()
	}()
	defer func() {
		os.Stdout = originalStdout
	}()
	function()
	_ = writer.Close()
	output := <-done
	_ = reader.Close()
	return output
}

func outputModes(t *testing.T, test func(t *testing.T, terminal bool)) {
	t.Helper()
	for _, terminal := range []bool{false, true} {
		name := "plain"
		if terminal {
			name = "styled"
		}
		t.Run(name, func(t *testing.T) {
			if terminal && runtime.GOOS == "windows" {
				t.Skip("pseudo-terminals require Unix")
			}
			test(t, terminal)
		})
	}
}

func TestParseDuration(t *testing.T) {
	tests := []struct {
		input         string
		expected      time.Duration
		expectedError bool
	}{
		{input: "30d", expected: 30 * 24 * time.Hour},
		{input: "72h", expected: 72 * time.Hour},
		{input: "0h", expected: 0},
		{input: "d", expectedError: true},
		{input: "", expectedError: true},
		{input: "xd", expectedError: true},
		{input: "5m", expectedError: true},
	}
	for _, test := range tests {
		t.Run(test.input, func(t *testing.T) {
			duration, err := parseDuration(test.input)
			if test.expectedError {
				if err == nil {
					t.Fatalf("expected error for %q, got %v", test.input, duration)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if duration != test.expected {
				t.Fatalf("expected %v, got %v", test.expected, duration)
			}
		})
	}
}

func TestNormalizeCommand(t *testing.T) {
	tests := []struct {
		input         string
		expected      string
		expectedError bool
	}{
		{input: "", expected: ""},
		{input: "build", expected: "build"},
		{input: "test", expected: "test"},
		{input: "run", expected: "run"},
		{input: "deploy", expectedError: true},
	}
	for _, test := range tests {
		t.Run(test.input, func(t *testing.T) {
			command, err := normalizeCommand(test.input)
			if (err != nil) != test.expectedError {
				t.Fatalf("unexpected error state: %v", err)
			}
			if command != test.expected {
				t.Fatalf("expected %q, got %q", test.expected, command)
			}
		})
	}
}

func TestNormalizeStatsCommandType(t *testing.T) {
	tests := []struct {
		input         string
		expected      string
		expectedError bool
	}{
		{input: "", expected: ""},
		{input: "all", expected: ""},
		{input: "build", expected: "build"},
		{input: "test", expected: "test"},
		{input: "run", expectedError: true},
	}
	for _, test := range tests {
		t.Run(test.input, func(t *testing.T) {
			commandType, err := normalizeStatsCommandType(test.input)
			if (err != nil) != test.expectedError {
				t.Fatalf("unexpected error state: %v", err)
			}
			if commandType != test.expected {
				t.Fatalf("expected %q, got %q", test.expected, commandType)
			}
		})
	}
}

func TestNormalizeStatsCI(t *testing.T) {
	trueValue, falseValue := true, false
	tests := []struct {
		input         string
		expected      *bool
		expectedError bool
	}{
		{input: "", expected: nil},
		{input: "all", expected: nil},
		{input: "true", expected: &trueValue},
		{input: "false", expected: &falseValue},
		{input: "maybe", expectedError: true},
	}
	for _, test := range tests {
		t.Run(test.input, func(t *testing.T) {
			isCI, err := normalizeStatsCI(test.input)
			if (err != nil) != test.expectedError {
				t.Fatalf("unexpected error state: %v", err)
			}
			if (isCI == nil) != (test.expected == nil) || (isCI != nil && *isCI != *test.expected) {
				t.Fatalf("expected %v, got %v", test.expected, isCI)
			}
		})
	}
}

func TestFormatDuration(t *testing.T) {
	tests := []struct {
		input    time.Duration
		expected string
	}{
		{input: 250 * time.Millisecond, expected: "250ms"},
		{input: 1500 * time.Millisecond, expected: "1.5s"},
	}
	for _, test := range tests {
		t.Run(test.expected, func(t *testing.T) {
			if formatted := formatDuration(test.input); formatted != test.expected {
				t.Fatalf("expected %q, got %q", test.expected, formatted)
			}
		})
	}
}

func TestRenderHelpers(t *testing.T) {
	outputModes(t, func(t *testing.T, terminal bool) {
		var rendered []string
		captureOutput(t, terminal, func() {
			rendered = []string{
				renderImpact(20000),
				renderImpact(5000),
				renderImpact(100),
				renderSection("section"),
				renderHint("hint"),
				renderLabel("//pkg:target"),
				renderDim("dim"),
			}
		})
		for _, expected := range []string{"20000ms", "5000ms", "100ms", "section", "hint", "//pkg:target", "dim"} {
			if !strings.Contains(strings.Join(rendered, "\n"), expected) {
				t.Errorf("expected rendered output to contain %q, got %q", expected, rendered)
			}
		}
	})
}

func TestSortSpans(t *testing.T) {
	spans := []tracing.SpanRow{
		{Label: "a", TotalDurationMillis: 1, CommandDurationMillis: 3, QueueWaitMillis: 2, HashDurationMillis: 1},
		{Label: "b", TotalDurationMillis: 3, CommandDurationMillis: 1, QueueWaitMillis: 1, HashDurationMillis: 2},
		{Label: "c", TotalDurationMillis: 2, CommandDurationMillis: 2, QueueWaitMillis: 3, HashDurationMillis: 3},
	}
	tests := []struct {
		sortBy   string
		expected string
	}{
		{sortBy: "total", expected: "bca"},
		{sortBy: "command", expected: "acb"},
		{sortBy: "queue", expected: "cab"},
		{sortBy: "hash", expected: "cba"},
	}
	for _, test := range tests {
		t.Run(test.sortBy, func(t *testing.T) {
			sorted := append([]tracing.SpanRow(nil), spans...)
			sortSpans(sorted, test.sortBy)
			var labels string
			for _, span := range sorted {
				labels += span.Label
			}
			if labels != test.expected {
				t.Fatalf("expected order %q, got %q", test.expected, labels)
			}
		})
	}
}

func TestPrintBuildSummary(t *testing.T) {
	builds := []tracing.BuildRow{
		{
			TraceID:                 "abcdef123456",
			Command:                 "build",
			GrogVersion:             "1.0.0",
			Platform:                "linux/amd64",
			GitCommit:               "deadbeef",
			GitBranch:               "main",
			TotalDurationMillis:     1500,
			TotalTargets:            3,
			CacheHitCount:           1,
			FailureCount:            1,
			CriticalPathExecMillis:  800,
			CriticalPathCacheMillis: 200,
			RequestedPatterns:       "//a/...,//b/...",
		},
		{TraceID: "fedcba654321", Command: "test", TotalTargets: 1},
	}
	outputModes(t, func(t *testing.T, terminal bool) {
		for _, build := range builds {
			output := captureOutput(t, terminal, func() { printBuildSummary(&build) })
			if !strings.Contains(output, build.TraceID) {
				t.Errorf("expected summary to contain trace id %q, got %q", build.TraceID, output)
			}
			if build.RequestedPatterns != "" && !strings.Contains(output, "//a/..., //b/...") {
				t.Errorf("expected summary to list patterns, got %q", output)
			}
		}
	})
}

func TestPrintSpanTable(t *testing.T) {
	spans := []tracing.SpanRow{
		{Label: "//pkg:failed", Status: "FAILURE", CacheResult: "CACHE_MISS", TotalDurationMillis: 3000},
		{Label: "//pkg:cancelled", Status: "CANCELLED", CacheResult: "CACHE_SKIP", TotalDurationMillis: 2000},
		{Label: "//pkg:hit", Status: "SUCCESS", CacheResult: "CACHE_HIT", TotalDurationMillis: 1000},
	}
	originalShowTop := showTop
	t.Cleanup(func() { showTop = originalShowTop })
	outputModes(t, func(t *testing.T, terminal bool) {
		showTop = 0
		t.Run("empty", func(t *testing.T) {
			if output := captureOutput(t, terminal, func() { printSpanTable(nil) }); output != "" {
				t.Fatalf("expected no output, got %q", output)
			}
		})
		t.Run("all", func(t *testing.T) {
			output := captureOutput(t, terminal, func() { printSpanTable(spans) })
			for _, expected := range []string{"TARGET", "//pkg:failed", "FAIL", "//pkg:cancelled", "//pkg:hit", "hit"} {
				if !strings.Contains(output, expected) {
					t.Errorf("expected table to contain %q, got %q", expected, output)
				}
			}
		})
		t.Run("top", func(t *testing.T) {
			showTop = 1
			output := captureOutput(t, terminal, func() { printSpanTable(spans) })
			if !strings.Contains(output, "//pkg:failed") || strings.Contains(output, "//pkg:hit") {
				t.Fatalf("expected only the first span, got %q", output)
			}
		})
	})
}

func TestPrintStatsSummary(t *testing.T) {
	statsList := []tracing.TraceStats{
		{TraceCount: 5, AvgDuration: 1200, CacheHitRate: 80, TotalFails: 0},
		{TraceCount: 5, AvgDuration: 300, CacheHitRate: 50, TotalFails: 2},
		{TraceCount: 5, AvgDuration: 300, CacheHitRate: 10, TotalFails: 2},
	}
	outputModes(t, func(t *testing.T, terminal bool) {
		for _, stats := range statsList {
			output := captureOutput(t, terminal, func() { printStatsSummary(&stats) })
			if !strings.Contains(output, "Stats over last 5 traces:") {
				t.Errorf("expected stats title, got %q", output)
			}
		}
	})
}

func TestPrintBottleneckReport(t *testing.T) {
	target := tracing.TargetBottleneck{
		Label:     "//pkg:slow",
		Count:     4,
		Frequency: 0.5,
		Impact:    12000,
		AvgCmd:    2000,
		AvgQueue:  700,
		AvgIO:     1500,
		AvgHash:   300,
		MissRate:  90,
		Failures:  2,
	}
	report := &tracing.BottleneckReport{
		SlowestTargets:       []tracing.TargetBottleneck{target},
		QueueSaturated:       []tracing.TargetBottleneck{target},
		IOBottlenecks:        []tracing.TargetBottleneck{target},
		SlowHashing:          []tracing.TargetBottleneck{target},
		FrequentMisses:       []tracing.TargetBottleneck{target},
		FlakyTargets:         []tracing.TargetBottleneck{target},
		OverallCacheMissRate: 40,
	}
	outputModes(t, func(t *testing.T, terminal bool) {
		output := captureOutput(t, terminal, func() { printBottleneckReport(report) })
		for _, expected := range []string{
			"Highest impact targets",
			"Worker pool saturation",
			"consider increasing num_workers",
			"I/O bottlenecks",
			"Slow hashing",
			"Frequent cache misses (>70% miss rate, overall avg: 40%)",
			"Frequently failing targets",
			"2/4",
		} {
			if !strings.Contains(output, expected) {
				t.Errorf("expected report to contain %q, got %q", expected, output)
			}
		}
	})
}
