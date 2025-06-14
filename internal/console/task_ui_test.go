package console

import (
	"context"
	tea "github.com/charmbracelet/bubbletea"
	"testing"
	"time"
)

func TestStartTaskUICtrlCCancelsContext(t *testing.T) {
	parent := context.Background()
	ctx, program, _ := StartTaskUI(parent)

	// ensure program has started
	time.Sleep(10 * time.Millisecond)

	program.Send(tea.KeyMsg{Type: tea.KeyCtrlC})

	select {
	case <-ctx.Done():
		// expected
	case <-time.After(time.Second):
		t.Fatal("context not cancelled after ctrl+c")
	}
}
