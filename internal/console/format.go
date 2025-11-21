package console

import (
	"fmt"

	"github.com/fatih/color"
	"go.uber.org/zap/zapcore"
)

// Pl small pluralization helper
func Pl(s string, count int) string {
	if count == 1 {
		return s
	}
	return s + "s"
}

// FCount Format a count
func FCount(count int, s string) string {
	return fmt.Sprintf("%d %s", count, Pl(s, count))
}

// FCountTargets Format a target count
func FCountTargets(count int) string {
	return FCount(count, "target")
}

// FCountOutputs Format a target count
func FCountOutputs(count int) string {
	return FCount(count, "output")
}

// FCountPkg Format a package count
func FCountPkg(count int) string {
	return FCount(count, "package")
}

func getMessagePrefix(level zapcore.Level) string {
	var levelText string

	cyan := color.New(color.FgCyan).SprintFunc()
	green := color.New(color.FgGreen).SprintFunc()
	yellow := color.New(color.FgYellow).SprintFunc()
	red := color.New(color.FgRed).SprintFunc()

	switch level {
	case zapcore.DebugLevel:
		levelText = cyan("DEBUG:") // Cyan
	case zapcore.InfoLevel:
		levelText = green("INFO:") // Green
	case zapcore.WarnLevel:
		levelText = yellow("WARN:") // Yellow
	case zapcore.ErrorLevel:
		levelText = red("ERROR:") // Red
	case zapcore.FatalLevel:
		levelText = red("FATAL:") // Red
	default:
		levelText = "UNKNOWN"
	}

	return levelText
}
