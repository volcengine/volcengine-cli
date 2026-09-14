//go:build !windows
// +build !windows

package proxycli

import (
	"errors"
	"os"
	"syscall"
)

func tryLockFile(file *os.File) error {
	return syscall.Flock(int(file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
}

func unlockFile(file *os.File) error {
	return syscall.Flock(int(file.Fd()), syscall.LOCK_UN)
}

func isLockBusy(err error) bool {
	return errors.Is(err, syscall.EWOULDBLOCK) || errors.Is(err, syscall.EAGAIN)
}
