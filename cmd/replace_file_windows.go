//go:build windows
// +build windows

package cmd

import (
	"errors"
	"syscall"
	"time"
	"unsafe"
)

const (
	moveFileReplaceExisting = 0x1
	moveFileWriteThrough    = 0x8
	replaceFileWait         = 100 * time.Millisecond
	replaceFileRetryDelay   = 5 * time.Millisecond
)

var (
	errorAccessDenied     = syscall.Errno(5)
	errorSharingViolation = syscall.Errno(32)
)

var moveFileExW = syscall.NewLazyDLL("kernel32.dll").NewProc("MoveFileExW")

// replaceFile atomically installs src at dst without deleting dst first.
// MOVEFILE_REPLACE_EXISTING is required because os.Rename cannot overwrite an
// existing file on Windows. If MoveFileExW fails, the existing dst is retained.
func replaceFile(src, dst string) error {
	srcPtr, err := syscall.UTF16PtrFromString(src)
	if err != nil {
		return err
	}
	dstPtr, err := syscall.UTF16PtrFromString(dst)
	if err != nil {
		return err
	}
	deadline := time.Now().Add(replaceFileWait)
	for {
		r1, _, callErr := moveFileExW.Call(
			uintptr(unsafe.Pointer(srcPtr)),
			uintptr(unsafe.Pointer(dstPtr)),
			uintptr(moveFileReplaceExisting|moveFileWriteThrough),
		)
		if r1 != 0 {
			return nil
		}
		err = moveFileExFailureError(callErr)
		// Antivirus scanners and readers built with older Go versions can
		// briefly hold the destination without delete sharing. Match the
		// bounded retry used by upgrade cache replacement without ever
		// deleting dst first.
		if !isReplaceFileRetryable(err) || !time.Now().Before(deadline) {
			return err
		}
		time.Sleep(replaceFileRetryDelay)
	}
}

func isReplaceFileRetryable(err error) bool {
	return errors.Is(err, errorAccessDenied) || errors.Is(err, errorSharingViolation)
}

// Windows APIs are allowed to report failure without setting the thread's
// last-error value. Proc.Call represents that as nil or syscall.Errno(0); do
// not let either case turn a failed MoveFileExW call into a nil Go error.
func moveFileExFailureError(callErr error) error {
	if callErr == nil || callErr == syscall.Errno(0) {
		return syscall.EINVAL
	}
	return callErr
}
