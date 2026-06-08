package console

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"grog/internal/config"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/fatih/color"
	"github.com/spf13/viper"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest/observer"
)

func newTraceLogger(level zapcore.Level) (*Logger, *observer.ObservedLogs) {
	core, logs := observer.New(level)
	return NewFromSugared(zap.New(core).Sugar(), level), logs
}

func TestLoggerTracefEnabledAndDisabled(t *testing.T) {
	traceLogger, logs := newTraceLogger(TraceLevel)
	traceLogger.Tracef("hello %s", "world")
	if logs.Len() != 1 {
		t.Fatalf("expected 1 trace entry, got %d", logs.Len())
	}
	if msg := logs.All()[0].Message; msg != "hello world" {
		t.Fatalf("unexpected message: %q", msg)
	}

	infoLogger, infoLogs := newTraceLogger(zap.InfoLevel)
	infoLogger.Tracef("ignored %s", "x")
	if infoLogs.Len() != 0 {
		t.Fatalf("expected trace to be filtered at info level, got %d entries", infoLogs.Len())
	}

	var nilLogger *Logger
	nilLogger.Tracef("safe on nil")
}

func TestLoggerDebugEnabled(t *testing.T) {
	debug, _ := newTraceLogger(zap.DebugLevel)
	if !debug.DebugEnabled() {
		t.Fatal("expected debug enabled at debug level")
	}

	info, _ := newTraceLogger(zap.InfoLevel)
	if info.DebugEnabled() {
		t.Fatal("expected debug disabled at info level")
	}

	var nilLogger *Logger
	if nilLogger.DebugEnabled() {
		t.Fatal("nil logger must not report debug enabled")
	}
}

func TestLoggerWithNamedWithOptions(t *testing.T) {
	base, logs := newTraceLogger(zap.InfoLevel)

	withFields := base.With("k", "v")
	if withFields == nil || withFields == base {
		t.Fatal("With should return a new Logger")
	}
	withFields.Infof("attached")

	named := base.Named("sub")
	if named == nil {
		t.Fatal("Named should return a Logger")
	}
	named.Infof("named")

	withOpts := base.WithOptions(zap.AddCallerSkip(1))
	if withOpts == nil {
		t.Fatal("WithOptions should return a Logger")
	}
	withOpts.Infof("opts")

	if logs.Len() < 3 {
		t.Fatalf("expected at least 3 entries, got %d", logs.Len())
	}
}

func TestWithLoggerReusesExistingValue(t *testing.T) {
	logger, _ := newTraceLogger(zap.InfoLevel)
	ctx := WithLogger(context.Background(), logger)
	again := WithLogger(ctx, logger)
	if ctx != again {
		t.Fatal("WithLogger should return the same context when the logger matches")
	}

	other, _ := newTraceLogger(zap.InfoLevel)
	swapped := WithLogger(ctx, other)
	if swapped == ctx {
		t.Fatal("WithLogger should produce a new context for a different logger")
	}
	if got := GetLogger(swapped); got != other {
		t.Fatal("GetLogger should return the most recently stored logger")
	}
}

func TestWarnOnError(t *testing.T) {
	logger, logs := newTraceLogger(zap.InfoLevel)
	ctx := WithLogger(context.Background(), logger)

	WarnOnError(ctx, func() error { return nil })
	if logs.Len() != 0 {
		t.Fatalf("expected no warning when func returns nil, got %d", logs.Len())
	}

	WarnOnError(ctx, func() error { return errors.New("boom") })
	if logs.Len() != 1 {
		t.Fatalf("expected one warning entry, got %d", logs.Len())
	}
	entry := logs.All()[0]
	if entry.Level != zap.WarnLevel {
		t.Fatalf("expected warn level, got %v", entry.Level)
	}
	if !strings.Contains(entry.Message, "boom") {
		t.Fatalf("expected message to mention error, got %q", entry.Message)
	}
}

func TestMustApplyColorSetting(t *testing.T) {
	prev := color.NoColor
	t.Cleanup(func() {
		color.NoColor = prev
		viper.Reset()
	})

	viper.Set("color", "yes")
	MustApplyColorSetting()
	if color.NoColor {
		t.Fatal("expected color enabled when setting is yes")
	}

	viper.Set("color", "no")
	MustApplyColorSetting()
	if !color.NoColor {
		t.Fatal("expected color disabled when setting is no")
	}

	viper.Set("color", "auto")
	MustApplyColorSetting()
}

func TestGetMessagePrefixAllLevels(t *testing.T) {
	color.NoColor = true
	t.Cleanup(func() { color.NoColor = false })

	levels := map[zapcore.Level]string{
		TraceLevel:         "TRACE:",
		zap.DebugLevel:     "DEBUG:",
		zap.InfoLevel:      "INFO:",
		zap.WarnLevel:      "WARN:",
		zap.ErrorLevel:     "ERROR:",
		zap.FatalLevel:     "FATAL:",
		zap.DPanicLevel:    "UNKNOWN",
		zapcore.PanicLevel: "UNKNOWN",
	}
	for lvl, want := range levels {
		if got := getMessagePrefix(lvl); !strings.Contains(got, want) {
			t.Errorf("level %v -> %q, want substring %q", lvl, got, want)
		}
	}
}

func TestInitLoggerLevels(t *testing.T) {
	prev := config.Global
	t.Cleanup(func() { config.Global = prev })

	for _, level := range []string{"trace", "debug", "info", "warn", "error", "bogus"} {
		config.Global = prev
		config.Global.LogLevel = level
		config.Global.LogOutputPath = "stdout"
		logger := InitLogger()
		if logger == nil {
			t.Fatalf("InitLogger returned nil for %q", level)
		}
	}

	config.Global = prev
	config.Global.LogLevel = ""
	config.Global.LogOutputPath = ""
	if logger := InitLogger(); logger == nil {
		t.Fatal("InitLogger returned nil for defaults")
	}
}

func TestGetTeaProgramAndLogger(t *testing.T) {
	if got := GetTeaProgram(context.Background()); got != nil {
		t.Fatal("expected nil program when none stored")
	}

	prev := config.Global
	t.Cleanup(func() { config.Global = prev })
	config.Global.LogLevel = "info"
	config.Global.LogOutputPath = "stdout"

	ctx := WithTeaLogger(context.Background(), nil)
	if got := GetTeaProgram(ctx); got != nil {
		t.Fatalf("expected GetTeaProgram to be nil when program is nil, got %v", got)
	}
	if logger := GetLogger(ctx); logger == nil {
		t.Fatal("expected non-nil logger after WithTeaLogger")
	}

	if logger := GetLogger(context.Background()); logger == nil {
		t.Fatal("GetLogger should fall back to a default logger")
	}
}

func TestProgressHelpers(t *testing.T) {
	cases := []struct {
		name     string
		p        Progress
		percent  int
		complete bool
		hasTotal bool
	}{
		{"zeroTotal", Progress{Current: 5, Total: 0}, 0, true, false},
		{"negativeCurrent", Progress{Current: -1, Total: 100}, 0, false, true},
		{"midway", Progress{Current: 50, Total: 100}, 50, false, true},
		{"complete", Progress{Current: 100, Total: 100}, 100, true, true},
		{"overshoot", Progress{Current: 200, Total: 100}, 100, true, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := c.p.percent(); got != c.percent {
				t.Errorf("percent = %d, want %d", got, c.percent)
			}
			if got := c.p.isComplete(); got != c.complete {
				t.Errorf("isComplete = %v, want %v", got, c.complete)
			}
			if got := c.p.hasTotal(); got != c.hasTotal {
				t.Errorf("hasTotal = %v, want %v", got, c.hasTotal)
			}
		})
	}
}

func TestProgressShouldRender(t *testing.T) {
	old := Progress{Current: 1, Total: 100, StartedAtSec: time.Now().Add(-time.Duration(RenderAfterSeconds+5) * time.Second).Unix()}
	if !old.shouldRender() {
		t.Fatal("expected progress that started long ago and is in-flight to render")
	}

	recent := Progress{Current: 1, Total: 100, StartedAtSec: time.Now().Unix()}
	if recent.shouldRender() {
		t.Fatal("expected fresh progress to suppress rendering")
	}

	done := Progress{Current: 100, Total: 100, StartedAtSec: time.Now().Add(-time.Minute).Unix()}
	if done.shouldRender() {
		t.Fatal("completed progress should not render")
	}

	noTotal := Progress{Current: 1, Total: 0, StartedAtSec: time.Now().Add(-time.Minute).Unix()}
	if noTotal.shouldRender() {
		t.Fatal("progress without a total should not render")
	}
}

func TestProgressFormatBarUnitCountAndZeroWidth(t *testing.T) {
	color.NoColor = true
	t.Cleanup(func() { color.NoColor = false })

	p := Progress{Current: 3, Total: 10, Unit: ProgressUnitCount}
	got := formatProgressBar(p, 12)
	if !strings.Contains(got, "3/10") {
		t.Fatalf("expected count suffix, got %q", got)
	}
	if !strings.Contains(got, " 30%") {
		t.Fatalf("expected percent in output, got %q", got)
	}

	if got := formatProgressBar(p, 0); got != "" {
		t.Fatalf("expected empty string for zero width, got %q", got)
	}
	if got := formatProgressBar(Progress{Current: 0, Total: 0}, 4); got != "" {
		t.Fatalf("expected empty string when no total, got %q", got)
	}
}

func TestFormatBytesLargerUnits(t *testing.T) {
	cases := map[int64]string{
		1024 * 1024:                      "1.0 MB",
		int64(1024) * 1024 * 1024:        "1.0 GB",
		int64(1024) * 1024 * 1024 * 1024: "1.0 TB",
	}
	for n, want := range cases {
		if got := formatBytes(n); got != want {
			t.Errorf("formatBytes(%d) = %q, want %q", n, got, want)
		}
	}
}

func TestResultLoggerFormatLabelTruncation(t *testing.T) {
	rl := NewResultLogger([]string{"//short", "//pretty-long-target-label-that-stretches"}, 20)
	short := rl.formatLabel("//short")
	if len(short) < rl.maxLabelWidth {
		t.Fatalf("short label not padded: %q (width %d)", short, rl.maxLabelWidth)
	}
	long := rl.formatLabel("//pretty-long-target-label-that-stretches-even-more")
	if !strings.HasPrefix(long, "...") {
		t.Fatalf("expected truncated label to start with ..., got %q", long)
	}

	tiny := NewResultLogger([]string{"a", "bb"}, 4)
	tiny.maxLabelWidth = 2
	got := tiny.formatLabel("123456789")
	if !strings.HasPrefix(got, "...") {
		t.Fatalf("expected ellipsis when too tight, got %q", got)
	}

	rl2 := &ResultLogger{maxLabelWidth: 1}
	if got := rl2.formatLabel("12345"); got != "..." {
		t.Fatalf("expected just ellipsis when truncation width <= 0, got %q", got)
	}
}

func TestGetResultLogger(t *testing.T) {
	if got := GetResultLogger(context.Background()); got != nil {
		t.Fatal("expected nil when no result logger present")
	}

	withConfig(t, "terse", false)
	rl := NewResultLogger(nil, 0)
	ctx := context.WithValue(context.Background(), ResultLoggerKey{}, rl)
	if got := GetResultLogger(ctx); got != rl {
		t.Fatalf("expected stored logger, got %v", got)
	}
}

func TestNewResultLoggerDefaultsTerminalWidth(t *testing.T) {
	withConfig(t, "terse", false)
	rl := NewResultLogger([]string{"//x"}, 0)
	if rl.terminalWidth != 80 {
		t.Fatalf("expected default terminal width of 80, got %d", rl.terminalWidth)
	}
}

func TestStreamLogsToggleNilSafety(t *testing.T) {
	var nilToggle *StreamLogsToggle
	if nilToggle.Enabled() {
		t.Fatal("nil toggle must report disabled")
	}
	if nilToggle.Toggle() {
		t.Fatal("nil toggle Toggle must return false")
	}

	ctx := WithStreamLogsToggle(context.Background(), nil)
	if ctx == nil {
		t.Fatal("WithStreamLogsToggle returned nil context")
	}
	if got := GetStreamLogsToggle(ctx); got != nil {
		t.Fatal("nil toggle should not be stored in context")
	}
}

func TestStreamLogsToggleConcurrentToggle(t *testing.T) {
	toggle := NewStreamLogsToggle(false)
	var wg sync.WaitGroup
	for range 32 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			toggle.Toggle()
		}()
	}
	wg.Wait()
	// 32 toggles starting from false leaves it false again (even count).
	if toggle.Enabled() {
		t.Fatalf("expected even toggles to leave disabled, got %v", toggle.Enabled())
	}
}

func TestStreamToggleWriterRespectsToggle(t *testing.T) {
	var sb strings.Builder
	toggle := NewStreamLogsToggle(false)
	w := NewStreamToggleWriter(&sb, toggle)

	n, err := w.Write([]byte("hidden"))
	if err != nil || n != len("hidden") {
		t.Fatalf("write returned (%d, %v)", n, err)
	}
	if sb.Len() != 0 {
		t.Fatalf("expected disabled writer to drop bytes, got %q", sb.String())
	}

	toggle.Toggle()
	if _, err := w.Write([]byte("visible")); err != nil {
		t.Fatal(err)
	}
	if got := sb.String(); got != "visible" {
		t.Fatalf("expected visible bytes to pass through, got %q", got)
	}

	bare := NewStreamToggleWriter(&sb, nil)
	if _, err := bare.Write([]byte("!")); err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(sb.String(), "!") {
		t.Fatalf("nil toggle should pass through, got %q", sb.String())
	}
}

func TestTeaWriterSync(t *testing.T) {
	out := captureStdout(t, func() {
		w := &TeaWriter{}
		_, _ = w.Write([]byte("partial"))
		if err := w.Sync(); err != nil {
			t.Fatalf("unexpected Sync error: %v", err)
		}
	})
	if !strings.Contains(out, "partial") {
		t.Fatalf("expected Sync to flush buffered data, got %q", out)
	}
}

func TestSetupCommandReturnsContext(t *testing.T) {
	ctx, logger := SetupCommand()
	if logger == nil {
		t.Fatal("SetupCommand should return a non-nil logger")
	}
	if ctx == nil {
		t.Fatal("SetupCommand should return a non-nil context")
	}
	if got := GetLogger(ctx); got != logger {
		t.Fatal("returned context should carry the returned logger")
	}
}

func TestUseTeaRespectsViper(t *testing.T) {
	t.Cleanup(viper.Reset)
	viper.Set("disable_tea", true)
	if UseTea() {
		t.Fatal("UseTea must return false when disable_tea is set")
	}
}

func TestModelUpdateUnknownKeyAndHeader(t *testing.T) {
	color.NoColor = true
	t.Cleanup(func() { color.NoColor = false })

	m := initialModel(context.Background(), make(chan tea.Msg, 1), func() {})
	if _, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'x'}}); cmd == nil {
		t.Fatal("expected listenForMsg cmd on unknown key")
	}
	if _, cmd := m.Update(HeaderMsg("header-text")); cmd == nil {
		t.Fatal("expected listenForMsg cmd after header")
	}
	if m.header != "header-text" {
		t.Fatalf("expected header to be set, got %q", m.header)
	}

	m.Update(TaskStateMsg{State: TaskStateMap{
		1: {Status: "build", StartedAtSec: time.Now().Add(-2 * time.Second).Unix()},
		2: {Status: "test", StartedAtSec: time.Now().Add(-2 * time.Second).Unix(), SubStatus: "sub"},
	}})
	view := m.View()
	if !strings.Contains(view, "build") || !strings.Contains(view, "test") {
		t.Fatalf("expected view to render tasks, got %q", view)
	}
	if !strings.Contains(view, "sub") {
		t.Fatalf("expected sub status to render, got %q", view)
	}

	mNoHeader := initialModel(context.Background(), make(chan tea.Msg, 1), func() {})
	mNoHeader.Update(TaskStateMsg{State: TaskStateMap{
		1: {Status: "alone", StartedAtSec: time.Now().Unix()},
	}})
	if view := mNoHeader.View(); !strings.Contains(view, "alone") {
		t.Fatalf("expected view without header to still render tasks, got %q", view)
	}

	m.Update(TickMsg(time.Now()))
}

func TestModelViewWithProgressAndStreamLabel(t *testing.T) {
	color.NoColor = true
	t.Cleanup(func() { color.NoColor = false })

	toggle := NewStreamLogsToggle(true)
	ctx := WithStreamLogsToggle(context.Background(), toggle)
	m := initialModel(ctx, make(chan tea.Msg, 1), func() {})
	m.header = "Header"

	started := time.Now().Add(-time.Duration(RenderAfterSeconds+3) * time.Second).Unix()
	m.Update(TaskStateMsg{State: TaskStateMap{
		1: {
			Status:       "task",
			SubStatus:    "details",
			StartedAtSec: started,
			Progress:     &Progress{Current: 5, Total: 10, StartedAtSec: started, Unit: ProgressUnitCount},
		},
	}})
	view := m.View()
	if !strings.Contains(view, "details") || !strings.Contains(view, "5/10") {
		t.Fatalf("expected progress + substatus, got %q", view)
	}
	if !strings.Contains(view, "top streaming logs") {
		t.Fatalf("expected stop streaming label, got %q", view)
	}
}
