//go:build windows
// +build windows

package upgrade

import (
	"errors"
	"syscall"
	"time"
	"unsafe"
)

const (
	checkCacheMoveFileReplaceExisting = 0x1
	checkCacheMoveFileWriteThrough    = 0x8
)

var checkCacheMoveFileExW = syscall.NewLazyDLL("kernel32.dll").NewProc("MoveFileExW")

const checkCacheReplaceWait = 100 * time.Millisecond

// replaceCheckCacheFile atomically installs src at dst without deleting dst
// first. If replacement fails (for example, a sharing violation), the previous
// cache remains present and src is left for the caller's deferred cleanup.
func replaceCheckCacheFile(src, dst string) error {
	srcPtr, err := syscall.UTF16PtrFromString(src)
	if err != nil {
		return err
	}
	dstPtr, err := syscall.UTF16PtrFromString(dst)
	if err != nil {
		return err
	}
	deadline := time.Now().Add(checkCacheReplaceWait)
	for {
		result, _, callErr := checkCacheMoveFileExW.Call(
			uintptr(unsafe.Pointer(srcPtr)),
			uintptr(unsafe.Pointer(dstPtr)),
			uintptr(checkCacheMoveFileReplaceExisting|checkCacheMoveFileWriteThrough),
		)
		if result != 0 {
			return nil
		}
		err = checkCacheMoveFileExFailureError(callErr)
		// A normal os.ReadFile handle does not share delete access on older Go
		// versions. Retry that short-lived conflict without ever removing dst.
		if !isCheckCacheReplaceRetryable(err) || !time.Now().Before(deadline) {
			return err
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func isCheckCacheReplaceRetryable(err error) bool {
	return errors.Is(err, syscall.Errno(5)) || errors.Is(err, syscall.Errno(32))
}

func checkCacheMoveFileExFailureError(callErr error) error {
	if callErr == nil || callErr == syscall.Errno(0) {
		return syscall.EINVAL
	}
	return callErr
}
