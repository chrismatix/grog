package console

import (
	"fmt"
	tea "github.com/charmbracelet/bubbletea"
	"strings"
)

// TeaWriter small helper so that we can use a tea.Program as a sync for zap
type TeaWriter struct{ Program *tea.Program }

func (w TeaWriter) Write(b []byte) (int, error) {
	outStr := strings.TrimRight(string(b), "\n")
	if useTea() {
		w.Program.Println(outStr)
	} else {
		// Log directly to stdout in non-tty (e.g. CI) environments
		fmt.Println(outStr)
	}

	return len(b), nil
}
func (w TeaWriter) Sync() error { return nil }
