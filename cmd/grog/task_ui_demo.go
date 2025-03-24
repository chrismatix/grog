package grog

import (
	"context"
	"fmt"
	"go.uber.org/zap/zapcore"
	"grog/internal/console"
	"os"
	"os/signal"
	"syscall"
	"time"
)

// --- Main Program ---
// Inspired by:
// https://github.com/charmbracelet/bubbletea/blob/main/examples/tui-daemon-combo/main.go
func OutputDemo() {
	// Initialize the Task UI.
	p, msgCh := console.StartTaskUI(context.TODO())

	// Handle SIGTERM signal.
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM) // Catch SIGINT and SIGTERM

	// Simulate some tasks.
	go func() {
		// Task 1
		msgCh <- console.HeaderMsg("Building target //app:server...")
		msgCh <- console.TaskStateMsg{State: map[int]console.TaskState{
			0: {Status: "[1/4] Fetching dependencies...", StartedAtSec: time.Now().Unix()},
		}}
		time.Sleep(2 * time.Second)
		msgCh <- console.LogMsg{Msg: "Downloading dependency github.com/example/lib1@v1.2.3", Level: zapcore.InfoLevel}
		msgCh <- console.LogMsg{Msg: "Downloading dependency github.com/example/lib2@v4.5.6", Level: zapcore.InfoLevel}
		time.Sleep(3 * time.Second)
		msgCh <- console.TaskStateMsg{State: map[int]console.TaskState{
			0: {Status: "[2/4] Compiling sources...", StartedAtSec: time.Now().Unix()},
		}}
		msgCh <- console.LogMsg{Msg: "Compiling main.go", Level: zapcore.InfoLevel}
		msgCh <- console.LogMsg{Msg: "Compiling utils.go", Level: zapcore.InfoLevel}
		time.Sleep(4 * time.Second)
		msgCh <- console.TaskStateMsg{State: map[int]console.TaskState{
			0: {Status: "[3/4] Running tests...", StartedAtSec: time.Now().Unix()},
		}}
		msgCh <- console.LogMsg{Msg: "Running unit tests for package app/utils", Level: zapcore.InfoLevel}
		msgCh <- console.LogMsg{Msg: "Test passed: TestHelperFunctions", Level: zapcore.InfoLevel}
		msgCh <- console.LogMsg{Msg: "Test failed: TestFeatureX", Level: zapcore.ErrorLevel}
		time.Sleep(3 * time.Second)
		msgCh <- console.TaskStateMsg{State: map[int]console.TaskState{
			0: {Status: "[4/4] Packaging build artifacts...", StartedAtSec: time.Now().Unix()},
		}}
		msgCh <- console.LogMsg{Msg: "Creating .tar.gz distribution", Level: zapcore.InfoLevel}
		msgCh <- console.LogMsg{Msg: "Build complete: saved to ./build/app-server.tar.gz", Level: zapcore.InfoLevel}
		time.Sleep(2 * time.Second)

		// Final Header
		msgCh <- console.HeaderMsg("Build completed successfully.")
	}()

	// Wait for a signal or timeout.
	select {
	case <-sigChan: // Caught SIGTERM or SIGINT
		fmt.Println("\nReceived interrupt signal! Shutting down...")
	case <-time.After(30 * time.Second): // Timeout after 30 seconds
		fmt.Println("\nTimeout reached! Shutting down...")
	}

	p.ReleaseTerminal() // Release the terminal
	p.Quit()            // Quit the Bubble Tea program
}
