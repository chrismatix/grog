package locking

import (
	"context"
	"errors"
	"fmt"
	"grog/internal/config"
	"grog/internal/console"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/fatih/color"
)

const lockFileFieldSeparator = "\t"

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
	lockData := []byte(buildLockFileContents(os.Getpid(), os.Args))
	waitPrinted := false

	if err := os.MkdirAll(filepath.Dir(wl.lockFilePath), 0755); err != nil {
		return fmt.Errorf("could not create workspace lock directory: %w", err)
	}

	for {
		logger.Debugf("Attempting to acquire workspace lock at %s", wl.lockFilePath)
		file, err := os.OpenFile(wl.lockFilePath, os.O_RDWR|os.O_CREATE|os.O_EXCL, 0644)
		if err == nil {
			_, writeErr := file.Write(lockData)
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
		otherPid, otherCommand, parseError := parseLockFileContents(string(data))
		if parseError != nil {
			_ = os.Remove(wl.lockFilePath)
			continue
		}
		if !processRunning(otherPid) {
			_ = os.Remove(wl.lockFilePath)
			continue
		}

		if !waitPrinted {
			green := color.New(color.FgGreen).SprintFunc()
			if otherCommand == "" {
				fmt.Printf("%s: Another grog build (PID %d) is running. Waiting..", green("INFO"), otherPid)
			} else {
				fmt.Printf(
					"%s: Another grog build (PID %d, command: %s) is running. Waiting..",
					green("INFO"),
					otherPid,
					otherCommand,
				)
			}
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

// ActiveLock describes a workspace lock that is currently held by a live
// process. It is returned by FindActiveLocks so callers (such as the clean
// command) can report which builds would be disrupted by a destructive
// operation on the shared cache.
type ActiveLock struct {
	// ProcessID is the PID recorded in the lock file.
	ProcessID int
	// Command is the command line recorded in the lock file, if any. It may
	// be empty for lock files written in the legacy PID-only format.
	Command string
	// LockFilePath is the absolute path of the lock file on disk.
	LockFilePath string
}

// FindActiveLocks scans the grog root directory for workspace lock files held
// by live processes. Each checkout stores its lock file at
// "$GROG_ROOT/<prefix>/lockfile" (see config.WorkspaceConfig.GetWorkspaceRootDir),
// so this walks one level below grogRoot and inspects every "lockfile" it
// finds. Lock files whose recorded process is no longer running are treated as
// stale and skipped, mirroring the stale-lock handling in Lock. The returned
// slice is sorted by lock file path for deterministic output.
func FindActiveLocks(grogRoot string) ([]ActiveLock, error) {
	entries, err := os.ReadDir(grogRoot)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("could not read grog root %q: %w", grogRoot, err)
	}

	var activeLocks []ActiveLock
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		lockFilePath := filepath.Join(grogRoot, entry.Name(), "lockfile")
		data, readError := os.ReadFile(lockFilePath)
		if readError != nil {
			// A missing lock file is the common case for non-workspace
			// directories (cas, cache, traces, ...); ignore it. Other read
			// errors are skipped too so a single unreadable file does not
			// abort the whole scan.
			continue
		}
		processID, command, parseError := parseLockFileContents(string(data))
		if parseError != nil {
			continue
		}
		if !processRunning(processID) {
			continue
		}
		activeLocks = append(activeLocks, ActiveLock{
			ProcessID:    processID,
			Command:      command,
			LockFilePath: lockFilePath,
		})
	}

	slices.SortFunc(activeLocks, func(a, b ActiveLock) int {
		return strings.Compare(a.LockFilePath, b.LockFilePath)
	})
	return activeLocks, nil
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

func buildLockFileContents(processID int, commandArguments []string) string {
	command := strings.TrimSpace(strings.Join(commandArguments, " "))
	if command == "" {
		return strconv.Itoa(processID)
	}
	return fmt.Sprintf("%d%s%s", processID, lockFileFieldSeparator, command)
}

func parseLockFileContents(lockFileContents string) (int, string, error) {
	trimmedContents := strings.TrimSpace(lockFileContents)
	if trimmedContents == "" {
		return 0, "", errors.New("lockfile is empty")
	}

	parts := strings.SplitN(trimmedContents, lockFileFieldSeparator, 2)
	processID, conversionError := strconv.Atoi(parts[0])
	if conversionError != nil {
		return 0, "", conversionError
	}
	if len(parts) < 2 {
		return processID, "", nil
	}

	return processID, strings.TrimSpace(parts[1]), nil
}
