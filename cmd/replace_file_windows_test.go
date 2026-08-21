//go:build windows
// +build windows

package cmd

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

func TestMoveFileExFailureAlwaysReturnsError(t *testing.T) {
	for _, callErr := range []error{nil, syscall.Errno(0)} {
		if err := moveFileExFailureError(callErr); err == nil {
			t.Fatalf("moveFileExFailureError(%v) returned nil", callErr)
		}
	}

	want := syscall.Errno(5)
	if got := moveFileExFailureError(want); !errors.Is(got, want) {
		t.Fatalf("moveFileExFailureError(%v) = %v, want original error", want, got)
	}
	for _, retryable := range []error{errorAccessDenied, errorSharingViolation} {
		if !isReplaceFileRetryable(retryable) {
			t.Fatalf("expected error %v to be retryable", retryable)
		}
	}
	if isReplaceFileRetryable(syscall.Errno(87)) {
		t.Fatal("invalid parameter must not be retryable")
	}
}

func TestReplaceFileWindowsSharingViolationPreservesExisting(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "replacement")
	dst := filepath.Join(dir, "config.json")
	wantOld := []byte("existing config must survive")
	wantNew := []byte("complete replacement")
	if err := os.WriteFile(src, wantNew, 0600); err != nil {
		t.Fatalf("write replacement: %v", err)
	}
	if err := os.WriteFile(dst, wantOld, 0600); err != nil {
		t.Fatalf("write existing config: %v", err)
	}

	dstPtr, err := syscall.UTF16PtrFromString(dst)
	if err != nil {
		t.Fatalf("encode destination path: %v", err)
	}
	// Keep the destination open without FILE_SHARE_DELETE. MoveFileExW must
	// reject the replacement with a sharing violation while both files remain.
	handle, err := syscall.CreateFile(
		dstPtr,
		syscall.GENERIC_READ,
		syscall.FILE_SHARE_READ|syscall.FILE_SHARE_WRITE,
		nil,
		syscall.OPEN_EXISTING,
		syscall.FILE_ATTRIBUTE_NORMAL,
		0,
	)
	if err != nil {
		t.Fatalf("open destination without delete sharing: %v", err)
	}
	started := time.Now()
	if err := replaceFile(src, dst); err == nil {
		_ = syscall.CloseHandle(handle)
		t.Fatal("replaceFile unexpectedly succeeded while destination denied delete sharing")
	}
	if elapsed := time.Since(started); elapsed < replaceFileWait {
		_ = syscall.CloseHandle(handle)
		t.Fatalf("sharing violation returned after %v, want retry window %v", elapsed, replaceFileWait)
	}
	if err := syscall.CloseHandle(handle); err != nil {
		t.Fatalf("close destination handle: %v", err)
	}

	gotOld, err := os.ReadFile(dst)
	if err != nil {
		t.Fatalf("read destination after failed replacement: %v", err)
	}
	if !bytes.Equal(gotOld, wantOld) {
		t.Fatalf("destination changed after failed replacement: got %q, want %q", gotOld, wantOld)
	}
	gotNew, err := os.ReadFile(src)
	if err != nil {
		t.Fatalf("replacement source was lost after failed replacement: %v", err)
	}
	if !bytes.Equal(gotNew, wantNew) {
		t.Fatalf("replacement source changed after failed replacement: got %q, want %q", gotNew, wantNew)
	}
}

func TestReplaceFileWindowsTransientSharingViolationSucceeds(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "replacement")
	dst := filepath.Join(dir, "config.json")
	wantNew := []byte("complete replacement")
	if err := os.WriteFile(src, wantNew, 0600); err != nil {
		t.Fatalf("write replacement: %v", err)
	}
	if err := os.WriteFile(dst, []byte("old config"), 0600); err != nil {
		t.Fatalf("write existing config: %v", err)
	}

	dstPtr, err := syscall.UTF16PtrFromString(dst)
	if err != nil {
		t.Fatalf("encode destination path: %v", err)
	}
	handle, err := syscall.CreateFile(
		dstPtr,
		syscall.GENERIC_READ,
		syscall.FILE_SHARE_READ|syscall.FILE_SHARE_WRITE,
		nil,
		syscall.OPEN_EXISTING,
		syscall.FILE_ATTRIBUTE_NORMAL,
		0,
	)
	if err != nil {
		t.Fatalf("open destination without delete sharing: %v", err)
	}

	closed := make(chan error, 1)
	time.AfterFunc(20*time.Millisecond, func() {
		closed <- syscall.CloseHandle(handle)
	})
	if err := replaceFile(src, dst); err != nil {
		closeErr := <-closed
		t.Fatalf("replaceFile after transient sharing violation: %v (close handle: %v)", err, closeErr)
	}
	if err := <-closed; err != nil {
		t.Fatalf("close destination handle: %v", err)
	}

	got, err := os.ReadFile(dst)
	if err != nil {
		t.Fatalf("read replaced destination: %v", err)
	}
	if !bytes.Equal(got, wantNew) {
		t.Fatalf("destination content = %q, want %q", got, wantNew)
	}
	if _, err := os.Stat(src); !os.IsNotExist(err) {
		t.Fatalf("replacement source still exists after success: %v", err)
	}
}
