package console

import (
	"context"
	"fmt"
	"maps"
	"os"
	"sort"
	"strings"
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
	SubStatus    string // optional secondary detail line rendered below status
	StartedAtSec int64
	Progress     *Progress
}

type TaskStateMap map[int]TaskState

type TaskStateMsg struct {
	State TaskStateMap
}

func StartTaskUI(ctx context.Context) (context.Context, *tea.Program, func(tea.Msg)) {
	msgCh := make(chan tea.Msg, 100)

	// Closed when the tea program stops consuming msgCh, so producers can
	// drop messages instead of blocking on a full channel after teardown.
	uiDone := make(chan struct{})

	// Because bubble tea manages the interrupt we overwrite
	// the default cancellation mechanism in cmd_setup.go
	wrappedCtx, cancel := context.WithCancel(ctx)

	var opts []tea.ProgramOption
	opts = append(opts, tea.WithContext(ctx))
	useTea := UseTea()
	if !useTea {
		// If we're in daemon mode don't render the TUI
		opts = append(opts, tea.WithInput(nil), tea.WithoutRenderer())
	}

	// Start the Bubbletea Program.
	p := tea.NewProgram(initialModel(ctx, msgCh, cancel), opts...)

	go func() {
		defer close(uiDone)

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
		case <-uiDone:
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

	streamLogsToggle *StreamLogsToggle
}

func initialModel(ctx context.Context, msgCh chan tea.Msg, cancel context.CancelFunc) *model {
	return &model{
		header:           "",
		tasks:            make(map[int]TaskState),
		msgCh:            msgCh,
		tick:             time.Now().Second(),
		cancel:           cancel,
		streamLogsToggle: GetStreamLogsToggle(ctx),
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
		case "s":
			if m.streamLogsToggle != nil {
				m.streamLogsToggle.Toggle()
				return m, nil
			}
		}
	case HeaderMsg:
		m.header = string(castMsg)
	case TaskStateMsg:
		m.tasksMutex.Lock()
		m.tasks = make(map[int]TaskState, len(castMsg.State))
		maps.Copy(m.tasks, castMsg.State)
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
	var s strings.Builder

	label := m.streamLogsCommandLabel()

	// Render header
	if m.header != "" || label != "" {
		if label == "" {
			s.WriteString(m.header + "\n")
		} else if m.header == "" {
			s.WriteString(label + "\n")
		} else {
			s.WriteString(m.header + ": " + label + "\n")
		}
	}

	// Render tasks in order:
	// Tasks may be sparse so we need to sort the task ids and then loop
	keys := make([]int, 0, len(m.tasks))
	for k := range m.tasks {
		keys = append(keys, k)
	}
	sort.Ints(keys)

	indent := "    "
	progressIndent := "     "
	if m.header == "" {
		indent = ""
		progressIndent = " "
	}

	dim := color.New(color.Faint).SprintFunc()
	for _, i := range keys {
		if status, ok := m.tasks[i]; ok {
			timePassed := int(time.Since(time.Unix(status.StartedAtSec, 0)).Seconds())
			s.WriteString(fmt.Sprintf("%s%s %ds\n", indent, status.Status, timePassed))
			hasProgress := status.Progress != nil && status.Progress.shouldRender()
			if status.SubStatus != "" {
				connector := "╰─"
				if hasProgress {
					connector = "├─"
				}
				s.WriteString(fmt.Sprintf("%s%s%s\n", progressIndent, connector, dim(status.SubStatus)))
			}
			if hasProgress {
				s.WriteString(fmt.Sprintf("%s╰─%s\n", progressIndent, formatProgressBar(*status.Progress, 24)))
			}
		}
	}

	return s.String()
}

func (m *model) streamLogsCommandLabel() string {
	if m.streamLogsToggle == nil {
		return ""
	}
	if m.streamLogsToggle.Enabled() {
		return color.New(color.Bold).Sprint("(s)") + "top streaming logs"
	}
	return color.New(color.Bold).Sprint("(s)") + "tream logs"
}
