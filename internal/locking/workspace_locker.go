package locking

import (
	"context"
	"errors"
	"fmt"
	"github.com/fatih/color"
	"grog/internal/config"
	"grog/internal/console"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

// WorkspaceLocker ensures that only one grog build is running per host by
// managing a lock file in the workspace root directory.
type WorkspaceLocker struct {
	lockFilePath string
	printOnce    sync.Once
}

// NewWorkspaceLocker creates a locker using the global configuration.
func NewWorkspaceLocker() *WorkspaceLocker {
	lockFilePath := filepath.Join(config.Global.GetWorkspaceRootDir(), "lockfile")
	return &WorkspaceLocker{lockFilePath: lockFilePath}
}

// Lock blocks until the workspace lock can be acquired.
func (wl *WorkspaceLocker) Lock(ctx context.Context) error {
	logger := console.GetLogger(ctx)
	pidStr := []byte(fmt.Sprintf("%d", os.Getpid()))
	waitPrinted := false

	for {
		logger.Debugf("Attempting to acquire workspace lock at %s", wl.lockFilePath)
		file, err := os.OpenFile(wl.lockFilePath, os.O_RDWR|os.O_CREATE|os.O_EXCL, 0644)
		if err == nil || errors.Is(err, os.ErrNotExist) {
			_, writeErr := file.Write(pidStr)
			file.Close()
			if writeErr != nil {
				os.Remove(wl.lockFilePath)
				return writeErr
			}
			return nil
		}
		if !errors.Is(err, os.ErrExist) {
			return err
		}

		// Read the lock file which contains the PID of the other process
		data, readError := os.ReadFile(wl.lockFilePath)
		if readError != nil {
			_ = os.Remove(wl.lockFilePath)
			continue
		}
		otherPid, conversionError := strconv.Atoi(strings.TrimSpace(string(data)))
		if conversionError != nil {
			_ = os.Remove(wl.lockFilePath)
			continue
		}
		if !processRunning(otherPid) {
			_ = os.Remove(wl.lockFilePath)
			continue
		}

		if waitPrinted == false {
			green := color.New(color.FgGreen).SprintFunc()
			fmt.Printf("%s: Another grog build (PID %d) is running. Waiting..", green("INFO"), otherPid)
			waitPrinted = true
			// Ensure that we add a newline when we printed anything
			defer fmt.Println()
		}
		fmt.Print(".")

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(1 * time.Second):
		}
	}
}

// Unlock releases the workspace lock.
func (wl *WorkspaceLocker) Unlock() error {
	return os.Remove(wl.lockFilePath)
}

func processRunning(pid int) bool {
	if pid <= 0 {
		return false
	}
	p, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	err = p.Signal(syscall.Signal(0))
	return err == nil || errors.Is(err, syscall.EPERM)
}
