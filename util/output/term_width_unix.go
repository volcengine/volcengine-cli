//go:build !windows
// +build !windows

package output

import (
	"os"
	"syscall"
	"unsafe"
)

// terminalWidth returns the terminal width of f, or 0 when unavailable.
//
// Uses the TIOCGWINSZ ioctl, the same mechanism as AWS CLI
// (determine_terminal_width in table.py), implemented without adding a
// dependency for a single syscall.
func terminalWidth(f *os.File) int {
	if f == nil {
		return 0
	}
	var ws struct {
		row, col, xpixel, ypixel uint16
	}
	_, _, errno := syscall.Syscall(
		syscall.SYS_IOCTL,
		f.Fd(),
		uintptr(syscall.TIOCGWINSZ),
		uintptr(unsafe.Pointer(&ws)),
	)
	if errno != 0 {
		return 0
	}
	return int(ws.col)
}
