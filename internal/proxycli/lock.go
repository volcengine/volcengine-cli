package proxycli

import (
	"fmt"
	"os"
	"path/filepath"
	"time"
)

type fileLock struct {
	file *os.File
}

func acquireFileLock(path string, timeout time.Duration) (*fileLock, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return nil, err
	}
	if info, err := os.Lstat(path); err == nil && !info.Mode().IsRegular() {
		return nil, fmt.Errorf("proxy CLI lock is not a regular file: %s", path)
	} else if err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, fmt.Errorf("open proxy CLI install lock: %w", err)
	}
	deadline := time.Now().Add(timeout)
	for {
		if err := tryLockFile(file); err == nil {
			return &fileLock{file: file}, nil
		} else if !isLockBusy(err) {
			_ = file.Close()
			return nil, fmt.Errorf("lock proxy CLI install state: %w", err)
		}
		if !time.Now().Before(deadline) {
			_ = file.Close()
			return nil, fmt.Errorf("timed out waiting for another proxy CLI installation")
		}
		time.Sleep(100 * time.Millisecond)
	}
}

func (l *fileLock) release() {
	if l == nil || l.file == nil {
		return
	}
	_ = unlockFile(l.file)
	_ = l.file.Close()
}
