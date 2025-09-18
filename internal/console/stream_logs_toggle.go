package console

import (
	"context"
	"sync/atomic"
)

type streamLogsToggleKey struct{}

// StreamLogsToggle manages the state for streaming target logs to the console.
type StreamLogsToggle struct {
	enabled atomic.Bool
}

// NewStreamLogsToggle returns a new toggle initialized with the provided value.
func NewStreamLogsToggle(initial bool) *StreamLogsToggle {
	toggle := &StreamLogsToggle{}
	toggle.enabled.Store(initial)
	return toggle
}

// Enabled reports whether streaming is currently enabled.
func (t *StreamLogsToggle) Enabled() bool {
	if t == nil {
		return false
	}
	return t.enabled.Load()
}

// Toggle flips the current state and returns the new value.
func (t *StreamLogsToggle) Toggle() bool {
	if t == nil {
		return false
	}
	for {
		current := t.enabled.Load()
		if t.enabled.CompareAndSwap(current, !current) {
			return !current
		}
	}
}

// WithStreamLogsToggle stores the toggle in the context for downstream use.
func WithStreamLogsToggle(ctx context.Context, toggle *StreamLogsToggle) context.Context {
	if toggle == nil {
		return ctx
	}
	return context.WithValue(ctx, streamLogsToggleKey{}, toggle)
}

// GetStreamLogsToggle retrieves the toggle stored in the context, if present.
func GetStreamLogsToggle(ctx context.Context) *StreamLogsToggle {
	if toggle, ok := ctx.Value(streamLogsToggleKey{}).(*StreamLogsToggle); ok {
		return toggle
	}
	return nil
}
