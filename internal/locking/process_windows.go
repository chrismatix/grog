package locking

import (
	"errors"

	"golang.org/x/sys/windows"
)

func processRunning(processID int) bool {
	if processID <= 0 {
		return false
	}
	processHandle, err := windows.OpenProcess(windows.SYNCHRONIZE, false, uint32(processID))
	if err != nil {
		return errors.Is(err, windows.ERROR_ACCESS_DENIED)
	}
	defer windows.CloseHandle(processHandle)
	status, err := windows.WaitForSingleObject(processHandle, 0)
	return err != nil || status == uint32(windows.WAIT_TIMEOUT)
}
