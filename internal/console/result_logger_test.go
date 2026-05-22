package console

import (
	"testing"
	"time"

	"github.com/fatih/color"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"

	"grog/internal/config"
)

func captureLogger() (*Logger, *observer.ObservedLogs) {
	core, logs := observer.New(zap.InfoLevel)
	return NewFromSugared(zap.New(core).Sugar(), zap.InfoLevel), logs
}

// withConfig sets the global output config for a test and restores it after.
func withConfig(t *testing.T, mode string, deterministic bool) {
	t.Helper()
	color.NoColor = true
	prevMode := config.Global.OutputMode
	prevDet := config.Global.DisableNonDeterministicLogging
	config.Global.OutputMode = mode
	config.Global.DisableNonDeterministicLogging = deterministic
	t.Cleanup(func() {
		config.Global.OutputMode = prevMode
		config.Global.DisableNonDeterministicLogging = prevDet
	})
}

func messages(logs *observer.ObservedLogs) []string {
	out := make([]string, 0, logs.Len())
	for _, e := range logs.All() {
		out = append(out, e.Message)
	}
	return out
}

func TestResultLoggerTerseStreaming(t *testing.T) {
	withConfig(t, "terse", false) // non-deterministic: timings shown, no buffering
	logger, logs := captureLogger()

	rl := NewResultLogger([]string{"//a:a", "//long:target"}, 80)
	rl.LogBuilt(logger, "//a:a", 1.23)
	rl.LogBuiltCached(logger, "//a:a", 0.1)
	rl.LogFailed(logger, "//a:a", 2*time.Second)
	rl.Flush(logger) // no-op when streaming

	got := messages(logs)
	want := []string{
		"//a:a         DONE in 1.2s", // padded to width of "//long:target" (13)
		"//a:a         DONE in 0.1s (cached)",
		"//a:a         FAILED in 2.0s",
	}
	if len(got) != len(want) {
		t.Fatalf("got %d lines %q, want %d", len(got), got, len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("line %d:\n got %q\nwant %q", i, got[i], want[i])
		}
	}
}

func TestResultLoggerTerseDeterministicBuffersAndSorts(t *testing.T) {
	withConfig(t, "terse", true) // deterministic: no timings, buffered + sorted
	logger, logs := captureLogger()

	rl := NewResultLogger([]string{"//b:b", "//a:a"}, 80)
	// Emitted out of order; Flush should sort by label and omit timings.
	rl.LogBuilt(logger, "//b:b", 9.9)
	rl.LogBuiltCached(logger, "//a:a", 9.9)

	if logs.Len() != 0 {
		t.Fatalf("expected lines to be buffered until Flush, got %q", messages(logs))
	}

	rl.Flush(logger)
	got := messages(logs)
	want := []string{
		"//a:a DONE (cached)",
		"//b:b DONE",
	}
	for i := range want {
		if i >= len(got) || got[i] != want[i] {
			t.Fatalf("got %q, want %q", got, want)
		}
	}
}

func TestResultLoggerDetailedInline(t *testing.T) {
	withConfig(t, "detailed", false)
	logger, logs := captureLogger()

	rl := NewResultLogger([]string{"//a:a", "//long:target"}, 80)
	rl.LogBuilt(logger, "//a:a", 1.23)
	rl.LogBuiltCached(logger, "//a:a", 0.1)
	rl.LogTestPassed(logger, "//a:a", 0.5)
	rl.LogFailed(logger, "//a:a", 2*time.Second)
	rl.Flush(logger) // detailed never buffers

	got := messages(logs)
	want := []string{
		"//a:a: done in 1.2s",          // no padding, lowercase verb
		"//a:a: done in 0.1s (cached)", // cache indicator last
		"//a:a: passed in 0.5s",
		"//a:a: failed in 2.0s",
	}
	if len(got) != len(want) {
		t.Fatalf("got %d lines %q, want %d", len(got), got, len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("line %d:\n got %q\nwant %q", i, got[i], want[i])
		}
	}
}
