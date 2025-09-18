package console

import (
	"context"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
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

func TestStartTaskUIToggleStreamLogs(t *testing.T) {
	parent := context.Background()
	toggle := NewStreamLogsToggle(false)
	ctxWithToggle := WithStreamLogsToggle(parent, toggle)
	_, program, _ := StartTaskUI(ctxWithToggle)

	// ensure program has started
	time.Sleep(10 * time.Millisecond)

	program.Send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'s'}})

	deadline := time.After(time.Second)
	for {
		if toggle.Enabled() {
			break
		}
		select {
		case <-deadline:
			t.Fatal("stream logs toggle did not enable after pressing 's'")
		default:
			time.Sleep(10 * time.Millisecond)
		}
	}

	program.Quit()
}

func TestModelViewShowsStreamLogsCommandBar(t *testing.T) {
	toggle := NewStreamLogsToggle(false)
	ctx := WithStreamLogsToggle(context.Background(), toggle)
	m := initialModel(ctx, nil, func() {})

	view := m.View()
	if !strings.Contains(view, "(s)tream logs") {
		t.Fatalf("expected view to include stream logs prompt, got: %q", view)
	}

	toggle.Toggle()

	view = m.View()
	if !strings.Contains(view, "(s)top streaming logs") {
		t.Fatalf("expected view to include stop streaming prompt, got: %q", view)
	}
}
