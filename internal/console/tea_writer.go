package console

import (
	"fmt"
	"io"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

// TeaWriter small helper so that we can use a tea.Program as a sync for zap
type TeaWriter struct {
	Program *tea.Program
	isDone  bool
}

func NewTeaWriter(program *tea.Program) *TeaWriter {
	w := &TeaWriter{
		Program: program,
		isDone:  false,
	}

	go func() {
		w.Program.Wait()
		w.isDone = true
	}()

	return w
}

func (w TeaWriter) Write(b []byte) (int, error) {
	outStr := strings.TrimRight(string(b), "\n")
	if UseTea() && !w.isDone {
		w.Program.Println(outStr)
	} else {
		// Log directly to stdout in non-tty (e.g. CI) environments
		fmt.Println(outStr)
	}

	return len(b), nil
}
func (w TeaWriter) Sync() error { return nil }

// StreamToggleWriter writes to the underlying writer only when the toggle is enabled.
type StreamToggleWriter struct {
	writer io.Writer
	toggle *StreamLogsToggle
}

// NewStreamToggleWriter wraps the provided writer with a toggle check.
func NewStreamToggleWriter(writer io.Writer, toggle *StreamLogsToggle) *StreamToggleWriter {
	return &StreamToggleWriter{writer: writer, toggle: toggle}
}

func (w *StreamToggleWriter) Write(p []byte) (int, error) {
	if w.toggle == nil || w.toggle.Enabled() {
		return w.writer.Write(p)
	}
	return len(p), nil
}
