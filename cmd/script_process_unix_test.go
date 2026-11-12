//go:build !windows
// +build !windows

package cmd

import "syscall"

// scriptProcessAlive reports whether pid still runs (signal 0 probe).
func scriptProcessAlive(pid int) bool {
	return syscall.Kill(pid, 0) == nil
}
