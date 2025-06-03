package console

import (
	"fmt"
	tea "github.com/charmbracelet/bubbletea"
	"strings"
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
