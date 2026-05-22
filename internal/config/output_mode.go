package config

import "fmt"

// OutputMode determines how per-target build/test progress is rendered in
// non-interactive (non-TUI) output.
type OutputMode int

const (
	// OutputModeTerse prints a single aligned completion line per target
	// (label, outcome, timing and cache indicator). This is the default.
	OutputModeTerse OutputMode = iota
	// OutputModeDetailed streams each target's lifecycle as it happens
	// (checking cache, running, writing outputs, done).
	OutputModeDetailed
)

// ParseOutputMode converts a string to an OutputMode. An empty string maps to
// the default (terse). Returns an error for any other unrecognized value.
func ParseOutputMode(s string) (OutputMode, error) {
	switch s {
	case "", "terse":
		return OutputModeTerse, nil
	case "detailed":
		return OutputModeDetailed, nil
	default:
		return OutputModeTerse, fmt.Errorf("invalid output_mode: '%s'. Must be either 'terse' or 'detailed'", s)
	}
}
