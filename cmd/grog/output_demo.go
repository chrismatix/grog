package grog

import (
	"fmt"
	"math/rand"
	"os"
	"sync"
	"time"
)

// Event represents a console update: either a task progress update or a general log message.
type Event struct {
	taskID  int    // Task ID for progress updates; -1 for general log messages
	message string // Text to display
	isLog   bool   // true for a log message, false for a task status update
}

// check out this demo:
// https://gist.github.com/lucapette/3462466c861e318b8a8dec8d29c38716
func OutputDemo() {
	// Example setup: 5 tasks, 3 workers
	const maxWorkers = 3
	tasks := []string{"Task 1", "Task 2", "Task 3", "Task 4", "Task 5"}
	numTasks := len(tasks)

	events := make(chan Event)
	var wg sync.WaitGroup

	// Goroutine to handle terminal output updates
	wg.Add(1)
	go func() {
		defer wg.Done()
		out := os.Stdout

		// Hide the cursor for a cleaner UI (optional)
		fmt.Fprint(out, "\x1b[?25l")
		defer fmt.Fprint(out, "\x1b[?25h") // ensure cursor is shown again at end

		// Print initial status lines for all tasks
		for _, name := range tasks {
			fmt.Fprintf(out, "%s: Pending\n", name)
		}
		tasksCount := numTasks // number of task lines (used for cursor math)

		// Listen for events and update the display
		for ev := range events {
			if ev.isLog {
				// Insert a new log line above the table
				fmt.Fprintf(out, "\x1b[%dA", tasksCount) // move cursor up to first task line
				fmt.Fprint(out, "\x1b[1L")               // insert a blank line here
				fmt.Fprintf(out, "%s\n", ev.message)     // print the log message in the new line
				fmt.Fprintf(out, "\x1b[%dB", tasksCount) // move cursor back down to bottom of table
			} else {
				// Update a task's progress line
				id := ev.taskID
				diff := tasksCount - id            // lines to move up from bottom to this task
				fmt.Fprintf(out, "\x1b[%dA", diff) // move up to the task's line
				fmt.Fprint(out, "\r\x1b[K")        // clear the line
				fmt.Fprint(out, ev.message)        // print the new status (no newline)
				fmt.Fprintf(out, "\x1b[%dB", diff) // move back down to the bottom of the table
			}
		}

		// After processing all events, move to a new line and print a completion message
		fmt.Fprintln(out)
		fmt.Fprintln(out, "All tasks completed.")
	}()

	// Worker pool: start workers that process tasks and send events
	taskCh := make(chan int)
	for w := 1; w <= maxWorkers; w++ {
		go func(workerID int) {
			for taskID := range taskCh {
				taskName := tasks[taskID]
				// Mark task as running
				events <- Event{taskID: taskID, message: fmt.Sprintf("%s: Running (worker %d)", taskName, workerID), isLog: false}

				// Simulate work with progress updates
				progress := 0
				fail := rand.Intn(100) < 20 // 20% chance to fail this task
				for progress < 100 && !fail {
					time.Sleep(100 * time.Millisecond)
					progress += 20
					if progress > 100 {
						progress = 100
					}
					events <- Event{taskID: taskID, message: fmt.Sprintf("%s: %d%%", taskName, progress), isLog: false}
				}

				// Task finished or failed
				if fail {
					// Log an error above the table
					events <- Event{taskID: -1, message: fmt.Sprintf("ERROR: %s encountered an issue", taskName), isLog: true}
					// Mark the task as failed in the table
					events <- Event{taskID: taskID, message: fmt.Sprintf("%s: FAILED", taskName), isLog: false}
				} else {
					// Mark task as done in the table
					events <- Event{taskID: taskID, message: fmt.Sprintf("%s: Done", taskName), isLog: false}
				}
			}
		}(w)
	}

	// Send all tasks into the taskCh for workers to process
	for i := 0; i < numTasks; i++ {
		taskCh <- i
	}
	close(taskCh) // no more tasks

	// Wait for a moment to ensure all task events are sent (in a real program, use sync.WaitGroup for tasks)
	time.Sleep(2 * time.Second)
	close(events) // no more events to process

	// Wait for the output goroutine to finish
	wg.Wait()
}
