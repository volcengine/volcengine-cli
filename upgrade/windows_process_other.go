//go:build !windows
// +build !windows

package upgrade

import (
	"fmt"
	"os/exec"
	"time"
)

func waitForProcessExit(pid int) error {
	if pid <= 0 {
		return nil
	}
	return fmt.Errorf("waiting for a Windows upgrade process is unsupported on this platform")
}

func configureBackgroundCommand(cmd *exec.Cmd) {}

func waitBeforeCleanupRetry() {
	time.Sleep(100 * time.Millisecond)
}
