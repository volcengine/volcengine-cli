//go:build !windows
// +build !windows

package scriptrun

import "syscall"

// processAlive reports whether pid still runs. The worker is reaped by the
// executor and orphaned children are reparented, so signal 0 is reliable.
func processAlive(pid int) bool {
	return syscall.Kill(pid, 0) == nil
}
