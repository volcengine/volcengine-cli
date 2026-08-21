//go:build windows
// +build windows

package cmd

import (
	"os"
	"syscall"
	"unsafe"
)

const configLockFileExclusiveLock = 0x00000002

var (
	kernel32ConfigLockFileEx   = syscall.NewLazyDLL("kernel32.dll").NewProc("LockFileEx")
	kernel32ConfigUnlockFileEx = syscall.NewLazyDLL("kernel32.dll").NewProc("UnlockFileEx")
)

func lockConfigFile(file *os.File) error {
	var overlapped syscall.Overlapped
	result, _, callErr := kernel32ConfigLockFileEx.Call(
		file.Fd(),
		configLockFileExclusiveLock,
		0,
		0xffffffff,
		0xffffffff,
		uintptr(unsafe.Pointer(&overlapped)),
	)
	if result == 0 {
		if callErr == nil || callErr == syscall.Errno(0) {
			return syscall.EINVAL
		}
		return callErr
	}
	return nil
}

func unlockConfigFile(file *os.File) error {
	var overlapped syscall.Overlapped
	result, _, callErr := kernel32ConfigUnlockFileEx.Call(
		file.Fd(),
		0,
		0xffffffff,
		0xffffffff,
		uintptr(unsafe.Pointer(&overlapped)),
	)
	if result == 0 {
		if callErr == nil || callErr == syscall.Errno(0) {
			return syscall.EINVAL
		}
		return callErr
	}
	return nil
}
