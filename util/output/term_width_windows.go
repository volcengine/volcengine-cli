//go:build windows
// +build windows

package output

import (
	"os"
	"syscall"
	"unsafe"
)

// terminalWidth returns the console width of f, or 0 when unavailable.
//
// Implemented with GetConsoleScreenBufferInfo directly instead of adding a
// dependency for one syscall. Reading the active console window preserves the
// user's actual width instead of relying on a fixed fallback.
func terminalWidth(f *os.File) int {
	if f == nil {
		return 0
	}
	type coord struct {
		x, y int16
	}
	type smallRect struct {
		left, top, right, bottom int16
	}
	type consoleScreenBufferInfo struct {
		size              coord
		cursorPosition    coord
		attributes        uint16
		window            smallRect
		maximumWindowSize coord
	}

	kernel32, err := syscall.LoadDLL("kernel32.dll")
	if err != nil {
		return 0
	}
	// LoadDLL increments the module reference count. terminalWidth can be
	// called repeatedly by library consumers, so release it on every path,
	// including a failed procedure lookup.
	defer kernel32.Release()
	proc, err := kernel32.FindProc("GetConsoleScreenBufferInfo")
	if err != nil {
		return 0
	}
	var info consoleScreenBufferInfo
	ret, _, _ := proc.Call(f.Fd(), uintptr(unsafe.Pointer(&info)))
	if ret == 0 {
		return 0
	}
	// window is inclusive on both ends.
	width := int(info.window.right) - int(info.window.left) + 1
	if width <= 0 {
		return 0
	}
	return width
}
