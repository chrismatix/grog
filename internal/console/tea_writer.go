package console

import (
	tea "github.com/charmbracelet/bubbletea"
	"strings"
)

// teaWriter small helper so that we can use a tea.Program as a sync for zap
type teaWriter struct{ p *tea.Program }

func (w teaWriter) Write(b []byte) (int, error) {
	w.p.Println(strings.TrimRight(string(b), "\n"))
	return len(b), nil
}
func (w teaWriter) Sync() error { return nil }
