package console

import (
	"context"
	"fmt"
	"os"
	"sort"
	"sync"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/fatih/color"
	"github.com/mattn/go-isatty"
	"github.com/spf13/viper"
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

func StartTaskUI(ctx context.Context) (context.Context, *tea.Program, func(tea.Msg)) {
	msgCh := make(chan tea.Msg, 100)

	// Because bubble tea manages the interrupt we overwrite
	// the default cancellation mechanism in cmd_setup.go
	wrappedCtx, cancel := context.WithCancel(ctx)

	var opts []tea.ProgramOption
	opts = append(opts, tea.WithContext(ctx))
	if !UseTea() {
		// If we're in daemon mode don't render the TUI
		opts = append(opts, tea.WithInput(nil), tea.WithoutRenderer())
	}

	// Start the Bubbletea Program.
	p := tea.NewProgram(initialModel(msgCh, cancel), opts...)

	go func() {
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
				fmt.Println("Failed to start Bubble Tea Program:", err)
			}
		}
	}()

	// inspired by program.Send
	sendFunc := func(msg tea.Msg) {
		select {
		case <-wrappedCtx.Done():
		case msgCh <- msg:
		}
	}

	// Finally, also add the tea logger
	return WithTeaLogger(wrappedCtx, p), p, sendFunc
}

func UseTea() bool {
	if viper.GetBool("disable_tea") {
		return false
	}
	return isatty.IsTerminal(os.Stdout.Fd())
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
	tasksMutex sync.RWMutex

	cancel context.CancelFunc
}

func initialModel(msgCh chan tea.Msg, cancel context.CancelFunc) *model {
	return &model{
		header: "",
		tasks:  make(map[int]TaskState),
		msgCh:  msgCh,
		tick:   time.Now().Second(),
		cancel: cancel,
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
	case tea.KeyMsg:
		switch castMsg.String() {
		// React to ctrl+c
		case "ctrl+c":
			m.cancel()
			green := color.New(color.FgGreen).SprintFunc()
			printMessage := fmt.Sprintf("%s: Received interrupt signal, exiting...", green("INFO"))
			return m, tea.Sequence(tea.Println(printMessage), tea.Quit)
		}
	case HeaderMsg:
		m.header = string(castMsg)
	case TaskStateMsg:
		m.tasksMutex.Lock()
		m.tasks = make(map[int]TaskState, len(castMsg.State))
		for k, v := range castMsg.State {
			m.tasks[k] = v
		}
		m.tasksMutex.Unlock()
	case TickMsg:
		m.tick++
		return m, tickEvery()
	}

	return m, listenForMsg(m.msgCh)
}

func (m *model) View() string {
	m.tasksMutex.RLock()
	defer m.tasksMutex.RUnlock()
	s := ""

	// Render header
	s += m.header + "\n"

	// Render tasks in order:
	// Tasks may be sparse so we need to sort the task ids and then loop
	keys := make([]int, 0, len(m.tasks))
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
