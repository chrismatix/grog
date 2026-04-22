//go:build !windows

package locking

import (
	"errors"
	"os"
	"syscall"
)

func processRunning(processID int) bool {
	if processID <= 0 {
		return false
	}
	process, err := os.FindProcess(processID)
	if err != nil {
		return false
	}
	err = process.Signal(syscall.Signal(0))
	return err == nil || errors.Is(err, syscall.EPERM)
}
