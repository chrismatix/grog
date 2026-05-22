package console

import (
	"context"
	"fmt"
	"sort"
	"strings"
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

// resultKind is the outcome rendered for a target.
type resultKind int

const (
	resultBuilt resultKind = iota
	resultPassed
	resultFailed
)

// ResultLogger renders one completion line per target. It has two styles,
// selected by config.OutputMode:
//
//   - terse (default): an aligned table — the label sits in a left column
//     padded to a common width, followed by the outcome (DONE/PASSED/FAILED),
//     the timing and an optional cache indicator. The cache indicator is
//     always appended last (after the timing) so the "<verb> in <t>s" column
//     stays vertically aligned whether or not a target was served from cache.
//
//   - detailed: an inline "<label>: <verb> in <t>s" line matching the lifecycle
//     status lines streamed during the build, so each target's final result
//     reads as the last step of its own stream.
//
// In deterministic logging mode (used by the integration fixtures) the order
// in which parallel workers finish is not stable, so completion lines (in
// either style) are buffered and emitted sorted by label on Flush. Otherwise
// lines are emitted as soon as each target completes so that long builds stream
// their progress.
type ResultLogger struct {
	maxLabelWidth int
	terminalWidth int
	detailed      bool

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
		detailed:      config.Global.GetOutputMode() == config.OutputModeDetailed,
		// In deterministic logging mode, buffer and sort completion lines by
		// label on Flush so concurrent completion order can't make output
		// flaky. This applies to both styles: the detailed lifecycle stream is
		// itself suppressed under deterministic logging, leaving only these
		// completion lines, which would otherwise race.
		buffered: config.Global.DisableNonDeterministicLogging,
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

// verb returns the colored outcome word for the current output style.
func (rl *ResultLogger) verb(kind resultKind) string {
	var word string
	var c *color.Color
	switch kind {
	case resultPassed:
		word, c = "PASSED", color.New(color.FgGreen)
	case resultFailed:
		word, c = "FAILED", color.New(color.FgRed)
	default: // resultBuilt
		word, c = "DONE", color.New(color.FgGreen)
	}
	if rl.detailed {
		// Lowercase to read as the last step of the lifecycle stream.
		word = strings.ToLower(word)
	}
	return c.Sprint(word)
}

// emit formats and either buffers or writes a single result line. The timing is
// omitted in deterministic logging mode to keep fixtures stable; the cache
// indicator is always appended last so the timing column stays aligned.
func (rl *ResultLogger) emit(logger *Logger, label string, kind resultKind, seconds float64, cached bool) {
	timing := ""
	if !config.Global.DisableNonDeterministicLogging {
		timing = fmt.Sprintf(" in %.1fs", seconds)
	}
	cachedSuffix := ""
	if cached {
		cachedSuffix = " (cached)"
	}

	var line string
	if rl.detailed {
		// Inline, matching the "<label>: <action>" lifecycle status lines.
		line = fmt.Sprintf("%s: %s%s%s", label, rl.verb(kind), timing, cachedSuffix)
	} else {
		line = fmt.Sprintf("%s %s%s%s", rl.formatLabel(label), rl.verb(kind), timing, cachedSuffix)
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
	rl.emit(logger, label, resultBuilt, seconds, false)
}

// LogBuiltCached logs a (non-test) target served from cache.
func (rl *ResultLogger) LogBuiltCached(logger *Logger, label string, seconds float64) {
	rl.emit(logger, label, resultBuilt, seconds, true)
}

// LogTestPassed logs a passing test target.
func (rl *ResultLogger) LogTestPassed(logger *Logger, label string, seconds float64) {
	rl.emit(logger, label, resultPassed, seconds, false)
}

// LogTestPassedCached logs a passing test target served from cache.
func (rl *ResultLogger) LogTestPassedCached(logger *Logger, label string, seconds float64) {
	rl.emit(logger, label, resultPassed, seconds, true)
}

// LogFailed logs a failed build or test target.
func (rl *ResultLogger) LogFailed(logger *Logger, label string, executionTime time.Duration) {
	rl.emit(logger, label, resultFailed, executionTime.Seconds(), false)
}
