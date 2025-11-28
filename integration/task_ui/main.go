package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"
	"time"

	"grog/internal/console"
)

// Vibe-coded demo entry point that spawns the Task UI and feeds it with some test tasks
// to simulate rendering and progress bars.
func main() {
	// Create a cancellable context that reacts to Ctrl+C
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	// Start the Task UI
	ctx, program, send := console.StartTaskUI(ctx)
	// Ensure we clean up the terminal on exit
	defer program.Quit()
	defer func() { _ = program.ReleaseTerminal() }()

	// Header
	send(console.HeaderMsg("Task UI Demo — press Ctrl+C to exit"))

	// Prepare some demo tasks
	type demoTask struct {
		id    int
		name  string
		cur   int64
		tot   int64
		start int64
		done  bool
	}

	now := time.Now().Unix()
	tasks := []demoTask{
		{id: 1, name: "Build //app:binary", cur: 0, tot: 50 * 1024 * 1024, start: now},       // 50 MB
		{id: 2, name: "Test //pkg/util:tests", cur: 0, tot: 20 * 1024 * 1024, start: now},    // 20 MB
		{id: 3, name: "Docker //images:backend", cur: 0, tot: 150 * 1024 * 1024, start: now}, // 150 MB
		{id: 5, name: "Lint //...", cur: 0, tot: 0, start: now},                              // no progress bar (no total)
	}

	ticker := time.NewTicker(150 * time.Millisecond)
	defer ticker.Stop()

	// End the demo automatically after some time if not interrupted
	demoTimeout := time.After(10 * time.Second)

	for {
		select {
		case <-ctx.Done():
			return
		case <-demoTimeout:
			return
		case <-ticker.C:
			// Update fake progress
			for i := range tasks {
				if tasks[i].done {
					continue
				}
				// Lint task just cycles status text without a bar
				if tasks[i].tot == 0 {
					// Toggle between states every ~1.2s
					if time.Now().UnixNano()/int64(time.Second)%2 == 0 {
						// noop, status will be set below
					}
				} else {
					// Increase by a chunk, slow down near the end
					inc := tasks[i].tot / 40 // ~4% per tick
					if tasks[i].cur > tasks[i].tot*90/100 {
						inc = tasks[i].tot / 120 // ~0.8% per tick near the end
					}
					tasks[i].cur += inc
					if tasks[i].cur >= tasks[i].tot {
						tasks[i].cur = tasks[i].tot
						tasks[i].done = true
					}
				}
			}

			// Build TaskState map to send to the UI
			state := make(console.TaskStateMap, len(tasks))
			for _, t := range tasks {
				status := t.name
				if t.done {
					status += ": DONE"
				} else if t.tot == 0 {
					// show elapsed seconds for tasks without progress bar
					status += ": running"
				}

				var progress *console.Progress
				if t.tot > 0 {
					p := console.Progress{
						StartedAtSec: t.start,
						Current:      t.cur,
						Total:        t.tot,
					}
					progress = &p
				}

				state[t.id] = console.TaskState{
					Status:       status,
					StartedAtSec: t.start,
					Progress:     progress,
				}
			}

			send(console.TaskStateMsg{State: state})
		}
	}
}
