//go:build windows
// +build windows

package cmd

import "os/exec"

func delegatedExitCode(err *exec.ExitError) int {
	return err.ExitCode()
}
