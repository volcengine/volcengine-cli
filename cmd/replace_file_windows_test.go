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
	if err := replaceFile(src, dst); err == nil {
		_ = syscall.CloseHandle(handle)
		t.Fatal("replaceFile unexpectedly succeeded while destination denied delete sharing")
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
