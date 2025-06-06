package main

import (
	"github.com/Netflix/go-expect"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"syscall"
	"testing"
	"time"
)

func TestInterruptHandling(t *testing.T) {
	// Get the path to the test repository
	repoPath := filepath.Join("./test_repos", "sleep")
	// Get the directory of the current file
	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatalf("problems recovering caller information")
	}
	integrationDir := filepath.Dir(filename)
	fullRepoPath := filepath.Join(integrationDir, repoPath)
	absRepoPath, err := filepath.Abs(fullRepoPath)
	if err != nil {
		t.Fatalf("could not get absolute path for repo: %v", err)
	}
	t.Logf("Using repository path: %s", absRepoPath)

	// clean the repository first
	cleanCmd := exec.Command(binaryPath, "clean")
	output, err := cleanCmd.CombinedOutput()
	if err != nil {
		t.Fatalf("could not clean repository: %v\n%s", err, output)
	}

	cmd := exec.Command(binaryPath, "build")
	cmd.Dir = absRepoPath

	// Set the environment variable for the coverage directory
	coverDir, err := getCoverDir()
	if err != nil {
		t.Fatalf("could not get coverage directory: %v", err)
	}
	cmd.Env = append(os.Environ(), "GOCOVERDIR="+coverDir)

	// Start the command
	err = cmd.Start()
	if err != nil {
		t.Fatalf("could not start command: %v", err)
	}

	// Wait a short time to ensure the command has started
	time.Sleep(500 * time.Millisecond)

	// Record the time before sending the interrupt
	startTime := time.Now()

	// Send interrupt signal to the process
	err = cmd.Process.Signal(syscall.SIGINT)
	if err != nil {
		t.Fatalf("could not send interrupt signal: %v", err)
	}

	err = cmd.Wait()

	endTime := time.Now()

	// Calculate the time it took to interrupt
	interruptTime := endTime.Sub(startTime)

	if interruptTime > 2*time.Second {
		t.Fatalf("interrupt took too long: %v", interruptTime)
	}

	if err == nil {
		t.Fatalf("expected command to fail with interrupt error, but it succeeded")
	}

	t.Logf("Command was interrupted successfully in %v", interruptTime)
}

func TestInterruptHandlingWithTTY(t *testing.T) {
	// Create a virtual TTY console
	console, err := expect.NewConsole()
	if err != nil {
		t.Fatalf("could not create console: %v", err)
	}
	defer console.Close()

	// Reuse the same repo path logic as in TestInterruptHandling
	repoPath := filepath.Join("./test_repos", "sleep")
	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatalf("problems recovering caller information")
	}
	integrationDir := filepath.Dir(filename)
	fullRepoPath := filepath.Join(integrationDir, repoPath)
	absRepoPath, err := filepath.Abs(fullRepoPath)
	if err != nil {
		t.Fatalf("could not get absolute path for repo: %v", err)
	}

	// Clean the repo first
	cleanCmd := exec.Command(binaryPath, "clean")
	if output, err := cleanCmd.CombinedOutput(); err != nil {
		t.Fatalf("could not clean repository: %v\n%s", err, output)
	}

	// Prepare the command under test
	cmd := exec.Command(binaryPath, "build")
	cmd.Dir = absRepoPath

	coverDir, err := getCoverDir()
	if err != nil {
		t.Fatalf("could not get coverage directory: %v", err)
	}
	cmd.Env = append(os.Environ(), "GOCOVERDIR="+coverDir)

	// Attach the console TTY to stdin/stdout/stderr
	cmd.Stdin = console.Tty()
	cmd.Stdout = console.Tty()
	cmd.Stderr = console.Tty()

	// Start the command
	if err := cmd.Start(); err != nil {
		t.Fatalf("could not start command: %v", err)
	}

	// Give it a moment to spin up
	time.Sleep(500 * time.Millisecond)

	// Send a Ctrl-C (ASCII 3) over the TTY
	startTime := time.Now()
	if err := cmd.Process.Signal(os.Interrupt); err != nil {
		t.Fatalf("could not send interrupt: %v", err)
	}

	// Wait for it to exit
	err = cmd.Wait()
	endTime := time.Now()

	// Verify it honored the interrupt quickly
	interruptTime := endTime.Sub(startTime)
	if interruptTime > 2*time.Second {
		t.Fatalf("interrupt took too long over TTY: %v", interruptTime)
	}
	if err == nil {
		t.Fatalf("expected command to fail with interrupt, but it succeeded")
	}
	t.Logf("Command was interrupted successfully over TTY in %v", interruptTime)
}
