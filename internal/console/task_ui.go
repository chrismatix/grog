package console

import (
	"context"
	"fmt"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/mattn/go-isatty"
	"io"
	"log"
	"os"
	"sort"
	"sync"
	"time"
)

// HeaderMsg What to display in the header
type HeaderMsg string

// TaskState reflects the current State of a task for display
type TaskState struct {
	Status       string
	StartedAtSec int64
}

type TaskStateMap map[int]TaskState

type TaskStateMsg struct {
	State TaskStateMap
}

func StartTaskUI(ctx context.Context) (*tea.Program, chan tea.Msg) {
	msgCh := make(chan tea.Msg, 100)

	// Disable input since we don't need it and also to keep our signal (sigterm) handler working
	opts := []tea.ProgramOption{tea.WithInput(nil)}
	if !isatty.IsTerminal(os.Stdout.Fd()) {
		// If we're in daemon mode don't render the TUI
		opts = append(opts, tea.WithoutRenderer())
	} else {
		// If we're in TUI mode, discard log output
		log.SetOutput(io.Discard)
	}

	// Start the Bubbletea program.
	p := tea.NewProgram(initialModel(msgCh), opts...)

	go func() {
		defer close(msgCh)
		errCh := make(chan error, 1)

		go func() {
			_, err := p.Run()
			errCh <- err
		}()

		select {
		case <-ctx.Done():
			p.Quit()
			return
		case err := <-errCh:
			if err != nil {
				fmt.Println("Failed to start Bubble Tea program:", err)
			}
		}
	}()

	return p, msgCh
}

// Bubbletea Model
type model struct {
	// the header message to keep updating
	header string
	// The tasks to keep updating
	tasks map[int]TaskState
	// The current tick (used for time display)
	tick int

	// Message channel for sending updates.
	msgCh chan tea.Msg

	// Mutex for the tasks map
	tasksMutex sync.Mutex
}

func initialModel(msgCh chan tea.Msg) *model {
	return &model{
		header: "",
		tasks:  make(map[int]TaskState),
		msgCh:  msgCh,
		tick:   time.Now().Second(),
	}
}

func (m *model) Init() tea.Cmd {
	// Start listening for messages and tick every second.
	return tea.Batch(listenForMsg(m.msgCh), tickEvery())
}

func listenForMsg(msgCh chan tea.Msg) tea.Cmd {
	return func() tea.Msg {
		return <-msgCh
	}
}

type TickMsg time.Time

func tickEvery() tea.Cmd {
	return tea.Tick(time.Second, func(t time.Time) tea.Msg {
		return TickMsg(t)
	})
}

func (m *model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch castMsg := msg.(type) {
	case HeaderMsg:
		m.header = string(castMsg)
	case TaskStateMsg:
		m.tasksMutex.Lock()
		m.tasks = castMsg.State
		m.tasksMutex.Unlock()
	case TickMsg:
		m.tick++
		return m, tickEvery()
	}

	return m, listenForMsg(m.msgCh)
}

func (m *model) View() string {
	s := ""

	// Render header
	s += m.header + "\n"

	// Render tasks in order.
	// tasks may be sparse so we need to sort the task ids and then loop
	keys := make([]int, 0, len(m.tasks))
	m.tasksMutex.Lock()
	defer m.tasksMutex.Unlock()
	for k := range m.tasks {
		keys = append(keys, k)
	}
	sort.Ints(keys)
	for _, i := range keys {
		if status, ok := m.tasks[i]; ok {
			timePassed := int(time.Since(time.Unix(status.StartedAtSec, 0)).Seconds())
			s += fmt.Sprintf("    %s %ds\n", status.Status, timePassed)
		}
	}
	return s
}
