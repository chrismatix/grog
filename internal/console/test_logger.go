package console

import (
	"context"
	"fmt"
	"time"
	"unicode/utf8"

	"github.com/fatih/color"
)

// TestLoggerKey context key for the TestLogger
type TestLoggerKey struct{}

// GetTestLogger retrieves the TestLogger from the context.
func GetTestLogger(ctx context.Context) *TestLogger {
	if logger, ok := ctx.Value(TestLoggerKey{}).(*TestLogger); ok {
		return logger
	}
	return nil
}

// TestLogger is a helper struct for formatting test target logs.
// It takes in the list of selected targets before a run and determines
// a table format so that any subsequent writes to that table will fit
// the targets on the left side while showing the status on the right.
type TestLogger struct {
	maxLabelWidth int
	terminalWidth int
}

// NewTestLogger creates a new TestLogger with the given target labels and terminal width.
// If terminalWidth is 0, it will use a default width.
func NewTestLogger(targetLabels []string, terminalWidth int) *TestLogger {
	// Default terminal width if not provided
	if terminalWidth <= 0 {
		terminalWidth = 80
	}

	// Calculate the maximum label width based on the targets
	maxLabelWidth := 0
	for _, label := range targetLabels {
		labelWidth := utf8.RuneCountInString(label)
		if labelWidth > maxLabelWidth {
			maxLabelWidth = labelWidth
		}
	}

	// Ensure the max label width is not too large (at most 70% of terminal width)
	maxAllowedWidth := int(float64(terminalWidth) * 0.7)
	if maxLabelWidth > maxAllowedWidth {
		maxLabelWidth = maxAllowedWidth
	}

	return &TestLogger{
		maxLabelWidth: maxLabelWidth,
		terminalWidth: terminalWidth,
	}
}

// formatLabel formats the target label to fit within the maximum width.
// If the label is too long, it will be truncated with ellipsis from the left.
func (tl *TestLogger) formatLabel(label string) string {
	labelWidth := utf8.RuneCountInString(label)
	if labelWidth <= tl.maxLabelWidth {
		// Pad with spaces to align all labels
		return fmt.Sprintf("%-*s", tl.maxLabelWidth, label)
	}

	// Truncate from the left with ellipsis
	ellipsis := "..."
	truncatedWidth := tl.maxLabelWidth - utf8.RuneCountInString(ellipsis)
	if truncatedWidth <= 0 {
		// If the max width is too small, just return the ellipsis
		return ellipsis
	}

	// Get the rightmost part of the label
	runes := []rune(label)
	return ellipsis + string(runes[len(runes)-truncatedWidth:])
}

// LogTestPassed logs a test target as passed.
func (tl *TestLogger) LogTestPassed(logger *Logger, label string, executionTime float64) {
	formattedLabel := tl.formatLabel(label)
	logger.Infof("%s %s in %.1fs", formattedLabel, color.New(color.FgGreen).Sprintf("PASSED"), executionTime)
}

// LogTestPassedCached logs a test target as passed (cached).
func (tl *TestLogger) LogTestPassedCached(logger *Logger, label string, executionTimeSeconds float64) {
	formattedLabel := tl.formatLabel(label)
	logger.Infof("%s %s (cached) in %.1fs", formattedLabel, color.New(color.FgGreen).Sprintf("PASSED"), executionTimeSeconds)
}

// LogTestFailed logs a test target as failed.
func (tl *TestLogger) LogTestFailed(logger *Logger, label string, executionTime time.Duration) {
	formattedLabel := tl.formatLabel(label)
	logger.Infof("%s %s in %.1fs", formattedLabel, color.New(color.FgRed).Sprintf("FAILED"), executionTime.Seconds())
}
