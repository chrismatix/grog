package console

import (
	"bytes"
	"fmt"
	"io"
	"strings"
	"sync"

	tea "github.com/charmbracelet/bubbletea"
)

// TeaWriter bridges a subprocess (or zap) byte stream into a tea.Program.
//
// It line-buffers: bytes are accumulated and only emitted on complete newline
// boundaries, so a single logical line that the OS pipe splits across multiple
// Write calls renders as one line instead of several. Carriage returns within a
// line are collapsed to the final segment, so in-place progress animations
// (download bars, etc.) render as one line rather than one line per redraw.
type TeaWriter struct {
	Program *tea.Program

	mu     sync.Mutex
	buf    []byte
	isDone bool
}

func NewTeaWriter(program *tea.Program) *TeaWriter {
	w := &TeaWriter{
		Program: program,
	}

	go func() {
		w.Program.Wait()
		w.mu.Lock()
		w.isDone = true
		w.mu.Unlock()
	}()

	return w
}

func (w *TeaWriter) Write(b []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	w.buf = append(w.buf, b...)
	for {
		i := bytes.IndexByte(w.buf, '\n')
		if i < 0 {
			break
		}
		line := string(w.buf[:i])
		w.buf = w.buf[i+1:]
		w.emit(collapseCarriageReturns(line))
	}
	return len(b), nil
}

// Flush emits any buffered partial line (one without a trailing newline). Call
// after the producing stream has closed so trailing output is not lost.
func (w *TeaWriter) Flush() {
	w.mu.Lock()
	defer w.mu.Unlock()

	if len(w.buf) == 0 {
		return
	}
	line := string(w.buf)
	w.buf = w.buf[:0]
	w.emit(collapseCarriageReturns(line))
}

// emit writes a single newline-free line. Callers must hold w.mu.
func (w *TeaWriter) emit(line string) {
	if UseTea() && !w.isDone {
		w.Program.Println(line)
	} else {
		// Log directly to stdout in non-tty (e.g. CI) environments
		fmt.Println(line)
	}
}

func (w *TeaWriter) Sync() error {
	w.Flush()
	return nil
}

// collapseCarriageReturns reduces a line redrawn in place via carriage returns
// to the text that would remain visible: the segment after the last \r.
func collapseCarriageReturns(line string) string {
	line = strings.TrimRight(line, "\r")
	if i := strings.LastIndexByte(line, '\r'); i >= 0 {
		line = line[i+1:]
	}
	return line
}

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
