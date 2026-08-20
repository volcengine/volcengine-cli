//go:build windows
// +build windows

package upgrade

import (
	"bytes"
	"errors"
	"io/ioutil"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

func TestCheckCacheMoveFileExFailureAlwaysReturnsError(t *testing.T) {
	for _, callErr := range []error{nil, syscall.Errno(0)} {
		if err := checkCacheMoveFileExFailureError(callErr); err == nil {
			t.Fatalf("checkCacheMoveFileExFailureError(%v) returned nil", callErr)
		}
	}

	want := syscall.Errno(5)
	if got := checkCacheMoveFileExFailureError(want); !errors.Is(got, want) {
		t.Fatalf("checkCacheMoveFileExFailureError(%v) = %v, want original error", want, got)
	}
	for _, retryable := range []error{syscall.Errno(5), syscall.Errno(32)} {
		if !isCheckCacheReplaceRetryable(retryable) {
			t.Fatalf("expected error %v to be retryable", retryable)
		}
	}
	if isCheckCacheReplaceRetryable(syscall.Errno(87)) {
		t.Fatal("invalid parameter must not be retryable")
	}
}

func TestWriteCheckCacheWindowsReplacesExistingFile(t *testing.T) {
	dir := t.TempDir()
	orig := ConfigDirFunc
	ConfigDirFunc = func() (string, error) { return dir, nil }
	defer func() { ConfigDirFunc = orig }()

	if err := SaveCheckCache("9.9.9", "1.0.0"); err != nil {
		t.Fatal(err)
	}
	if err := SaveCheckCache("9.9.10", "1.0.0"); err != nil {
		t.Fatalf("replace existing cache: %v", err)
	}
	c, ok := loadCheckCacheFile()
	if !ok || c.Latest != "9.9.10" || c.Current != "1.0.0" {
		t.Fatalf("cache after replacement: ok=%v c=%+v", ok, c)
	}
}

func TestReplaceCheckCacheFileWindowsSharingViolationPreservesExisting(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "replacement")
	dst := filepath.Join(dir, "version_check.json")
	wantOld := []byte("existing cache must survive")
	wantNew := []byte("complete replacement")
	if err := ioutil.WriteFile(src, wantNew, 0600); err != nil {
		t.Fatal(err)
	}
	if err := ioutil.WriteFile(dst, wantOld, 0600); err != nil {
		t.Fatal(err)
	}

	dstPtr, err := syscall.UTF16PtrFromString(dst)
	if err != nil {
		t.Fatal(err)
	}
	// Deny FILE_SHARE_DELETE so MoveFileExW must fail without changing either
	// the installed cache or the replacement source.
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
		t.Fatal(err)
	}
	started := time.Now()
	if err := replaceCheckCacheFile(src, dst); err == nil {
		_ = syscall.CloseHandle(handle)
		t.Fatal("replacement unexpectedly succeeded while delete sharing was denied")
	}
	if elapsed := time.Since(started); elapsed < checkCacheReplaceWait {
		_ = syscall.CloseHandle(handle)
		t.Fatalf("sharing violation returned after %v, want retry window %v", elapsed, checkCacheReplaceWait)
	}
	if err := syscall.CloseHandle(handle); err != nil {
		t.Fatal(err)
	}

	gotOld, err := ioutil.ReadFile(dst)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(gotOld, wantOld) {
		t.Fatalf("destination changed: got %q want %q", gotOld, wantOld)
	}
	gotNew, err := ioutil.ReadFile(src)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(gotNew, wantNew) {
		t.Fatalf("replacement changed: got %q want %q", gotNew, wantNew)
	}
}

func TestWriteCheckCacheWindowsSharingViolationPreservesExisting(t *testing.T) {
	dir := t.TempDir()
	orig := ConfigDirFunc
	ConfigDirFunc = func() (string, error) { return dir, nil }
	defer func() { ConfigDirFunc = orig }()

	if err := SaveCheckCache("9.9.9", "1.0.0"); err != nil {
		t.Fatal(err)
	}
	path, err := CheckCachePath()
	if err != nil {
		t.Fatal(err)
	}
	want, err := ioutil.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	pathPtr, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		t.Fatal(err)
	}
	handle, err := syscall.CreateFile(
		pathPtr,
		syscall.GENERIC_READ,
		syscall.FILE_SHARE_READ|syscall.FILE_SHARE_WRITE,
		nil,
		syscall.OPEN_EXISTING,
		syscall.FILE_ATTRIBUTE_NORMAL,
		0,
	)
	if err != nil {
		t.Fatal(err)
	}
	err = writeCheckCache(versionCheckCache{
		CheckedAt: time.Now().Unix(),
		Latest:    "9.9.10",
		Current:   "1.0.0",
	})
	if err == nil {
		_ = syscall.CloseHandle(handle)
		t.Fatal("write unexpectedly succeeded while delete sharing was denied")
	}
	if err := syscall.CloseHandle(handle); err != nil {
		t.Fatal(err)
	}
	got, err := ioutil.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("old cache changed after failed replacement: got=%q want=%q", got, want)
	}
	matches, err := filepath.Glob(filepath.Join(filepath.Dir(path), "."+filepath.Base(path)+".tmp-*"))
	if err != nil {
		t.Fatal(err)
	}
	if len(matches) != 0 {
		t.Fatalf("temporary cache files leaked: %v", matches)
	}
}
