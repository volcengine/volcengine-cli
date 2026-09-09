//go:build !windows
// +build !windows

package cmd

import (
	"os/exec"
	"syscall"
)

func delegatedExitCode(err *exec.ExitError) int {
	if status, ok := err.Sys().(syscall.WaitStatus); ok && status.Signaled() {
		return 128 + int(status.Signal())
	}
	return err.ExitCode()
}
