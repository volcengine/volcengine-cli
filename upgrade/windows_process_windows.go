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

// parentWaitTimeout bounds how long the Windows upgrade helper waits for the
// parent CLI process to exit before replacing the binary. Overridable in tests.
var parentWaitTimeout = 2 * time.Minute

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

	timeoutMs := uint32(parentWaitTimeout / time.Millisecond)
	event, err := syscall.WaitForSingleObject(handle, timeoutMs)
	if err != nil {
		return err
	}
	switch event {
	case syscall.WAIT_OBJECT_0:
		return nil
	case syscall.WAIT_TIMEOUT:
		return fmt.Errorf("timed out waiting for parent process %d to exit after %s", pid, parentWaitTimeout)
	default:
		return fmt.Errorf("unexpected wait result %d", event)
	}
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
