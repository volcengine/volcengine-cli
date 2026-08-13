//go:build windows
// +build windows

package upgrade

import (
	"errors"
	"os"
	"syscall"
	"unsafe"
)

const (
	lockFileFailImmediately = 0x00000001
	lockFileExclusiveLock   = 0x00000002
)

var (
	kernel32UnlockFileEx = syscall.NewLazyDLL("kernel32.dll").NewProc("UnlockFileEx")
	kernel32LockFileEx   = syscall.NewLazyDLL("kernel32.dll").NewProc("LockFileEx")
)

func tryLockUpgradeFile(file *os.File) error {
	var overlapped syscall.Overlapped
	result, _, callErr := kernel32LockFileEx.Call(
		file.Fd(),
		lockFileFailImmediately|lockFileExclusiveLock,
		0,
		0xffffffff,
		0xffffffff,
		uintptr(unsafe.Pointer(&overlapped)),
	)
	if result == 0 {
		return callErr
	}
	return nil
}

func unlockUpgradeFile(file *os.File) error {
	var overlapped syscall.Overlapped
	result, _, callErr := kernel32UnlockFileEx.Call(
		file.Fd(),
		0,
		0xffffffff,
		0xffffffff,
		uintptr(unsafe.Pointer(&overlapped)),
	)
	if result == 0 {
		return callErr
	}
	return nil
}

func isUpgradeLockBusy(err error) bool {
	return errors.Is(err, syscall.Errno(32)) || errors.Is(err, syscall.Errno(33))
}
