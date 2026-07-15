//go:build windows
// +build windows

package upgrade

import (
	"fmt"
	"os/exec"
	"syscall"
	"time"
)

const errorInvalidParameter syscall.Errno = 87

func waitForProcessExit(pid int) error {
	if pid <= 0 {
		return nil
	}
	handle, err := syscall.OpenProcess(syscall.SYNCHRONIZE, false, uint32(pid))
	if err != nil {
		// The process may have exited before the helper opened its handle.
		if err == errorInvalidParameter {
			return nil
		}
		return err
	}
	defer syscall.CloseHandle(handle)
	event, err := syscall.WaitForSingleObject(handle, syscall.INFINITE)
	if err != nil {
		return err
	}
	if event != syscall.WAIT_OBJECT_0 {
		return fmt.Errorf("unexpected wait result %d", event)
	}
	return nil
}

func configureBackgroundCommand(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{
		HideWindow:    true,
		CreationFlags: syscall.CREATE_NEW_PROCESS_GROUP,
	}
}

func waitBeforeCleanupRetry() {
	time.Sleep(100 * time.Millisecond)
}
