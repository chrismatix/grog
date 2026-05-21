package console

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"time"
	"unicode/utf8"

	"grog/internal/config"

	"github.com/fatih/color"
)

// ResultLoggerKey is the context key for the ResultLogger.
type ResultLoggerKey struct{}

// GetResultLogger retrieves the ResultLogger from the context.
func GetResultLogger(ctx context.Context) *ResultLogger {
	if logger, ok := ctx.Value(ResultLoggerKey{}).(*ResultLogger); ok {
		return logger
	}
	return nil
}

// ResultLogger formats one aligned completion line per target so that build
// and test results read like a table: the target label sits in a left column
// padded to a common width, followed by the outcome, the timing and an
// optional cache indicator.
//
// The cache indicator is always appended last (after the timing) so the
// "<verb> in <t>s" column stays vertically aligned whether or not a given
// target was served from cache.
//
// In deterministic logging mode (used by the integration fixtures) the order
// in which parallel workers finish is not stable, so lines are buffered and
// emitted sorted by label on Flush. Otherwise lines are emitted as soon as
// each target completes so that long builds stream their progress.
type ResultLogger struct {
	maxLabelWidth int
	terminalWidth int

	buffered bool
	mu       sync.Mutex
	lines    []bufferedLine
}

type bufferedLine struct {
	label string
	text  string
}

// NewResultLogger creates a ResultLogger sized to the given target labels.
// If terminalWidth is 0 a default width is used.
func NewResultLogger(targetLabels []string, terminalWidth int) *ResultLogger {
	if terminalWidth <= 0 {
		terminalWidth = 80
	}

	maxLabelWidth := 0
	for _, label := range targetLabels {
		if w := utf8.RuneCountInString(label); w > maxLabelWidth {
			maxLabelWidth = w
		}
	}

	// Keep the label column to at most 70% of the terminal width.
	if maxAllowed := int(float64(terminalWidth) * 0.7); maxLabelWidth > maxAllowed {
		maxLabelWidth = maxAllowed
	}

	return &ResultLogger{
		maxLabelWidth: maxLabelWidth,
		terminalWidth: terminalWidth,
		buffered:      config.Global.DisableNonDeterministicLogging,
	}
}

// formatLabel pads the label to the column width, or truncates it from the
// left with an ellipsis when it is too long.
func (rl *ResultLogger) formatLabel(label string) string {
	labelWidth := utf8.RuneCountInString(label)
	if labelWidth <= rl.maxLabelWidth {
		return fmt.Sprintf("%-*s", rl.maxLabelWidth, label)
	}

	ellipsis := "..."
	truncatedWidth := rl.maxLabelWidth - utf8.RuneCountInString(ellipsis)
	if truncatedWidth <= 0 {
		return ellipsis
	}
	runes := []rune(label)
	return ellipsis + string(runes[len(runes)-truncatedWidth:])
}

// emit formats and either buffers or writes a single result line:
//
//	<label> <verb>[ in <t>s][ (cached)]
//
// The timing is omitted in deterministic logging mode to keep fixtures stable.
func (rl *ResultLogger) emit(logger *Logger, label, verb string, seconds float64, cached bool) {
	line := fmt.Sprintf("%s %s", rl.formatLabel(label), verb)
	if !config.Global.DisableNonDeterministicLogging {
		line += fmt.Sprintf(" in %.1fs", seconds)
	}
	if cached {
		line += " (cached)"
	}

	if rl.buffered {
		rl.mu.Lock()
		rl.lines = append(rl.lines, bufferedLine{label: label, text: line})
		rl.mu.Unlock()
		return
	}
	logger.Infof("%s", line)
}

// Flush emits any buffered lines sorted by label. It is a no-op when lines are
// streamed (the non-deterministic default). Safe to call more than once.
func (rl *ResultLogger) Flush(logger *Logger) {
	if rl == nil || !rl.buffered {
		return
	}
	rl.mu.Lock()
	lines := rl.lines
	rl.lines = nil
	rl.mu.Unlock()

	sort.Slice(lines, func(i, j int) bool { return lines[i].label < lines[j].label })
	for _, l := range lines {
		logger.Infof("%s", l.text)
	}
}

// LogBuilt logs a freshly built (non-test) target.
func (rl *ResultLogger) LogBuilt(logger *Logger, label string, seconds float64) {
	rl.emit(logger, label, color.New(color.FgGreen).Sprintf("DONE"), seconds, false)
}

// LogBuiltCached logs a (non-test) target served from cache.
func (rl *ResultLogger) LogBuiltCached(logger *Logger, label string, seconds float64) {
	rl.emit(logger, label, color.New(color.FgGreen).Sprintf("DONE"), seconds, true)
}

// LogTestPassed logs a passing test target.
func (rl *ResultLogger) LogTestPassed(logger *Logger, label string, seconds float64) {
	rl.emit(logger, label, color.New(color.FgGreen).Sprintf("PASSED"), seconds, false)
}

// LogTestPassedCached logs a passing test target served from cache.
func (rl *ResultLogger) LogTestPassedCached(logger *Logger, label string, seconds float64) {
	rl.emit(logger, label, color.New(color.FgGreen).Sprintf("PASSED"), seconds, true)
}

// LogFailed logs a failed build or test target.
func (rl *ResultLogger) LogFailed(logger *Logger, label string, executionTime time.Duration) {
	rl.emit(logger, label, color.New(color.FgRed).Sprintf("FAILED"), executionTime.Seconds(), false)
}
