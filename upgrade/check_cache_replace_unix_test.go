//go:build !windows
// +build !windows

package upgrade

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

type stubCheckCacheParent struct {
	syncErr error
	synced  bool
	closed  bool
}

func (d *stubCheckCacheParent) Sync() error {
	d.synced = true
	return d.syncErr
}

func (d *stubCheckCacheParent) Close() error {
	d.closed = true
	return nil
}

func TestReplaceCheckCacheFileUnixSyncsParentDirectory(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "replacement")
	dst := filepath.Join(dir, "version_check.json")
	if err := os.WriteFile(src, []byte("new cache"), 0600); err != nil {
		t.Fatal(err)
	}

	originalOpen := openCheckCacheParent
	parent := &stubCheckCacheParent{}
	var openedPath string
	openCheckCacheParent = func(path string) (checkCacheParent, error) {
		openedPath = path
		return parent, nil
	}
	t.Cleanup(func() { openCheckCacheParent = originalOpen })

	if err := replaceCheckCacheFile(src, dst); err != nil {
		t.Fatal(err)
	}
	if openedPath != dir || !parent.synced || !parent.closed {
		t.Fatalf("parent path=%q synced=%v closed=%v", openedPath, parent.synced, parent.closed)
	}
}

func TestReplaceCheckCacheFileUnixReportsPartialCommitOnSyncFailure(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "replacement")
	dst := filepath.Join(dir, "version_check.json")
	want := []byte("new cache")
	if err := os.WriteFile(src, want, 0600); err != nil {
		t.Fatal(err)
	}

	wantErr := errors.New("injected directory sync failure")
	originalOpen := openCheckCacheParent
	parent := &stubCheckCacheParent{syncErr: wantErr}
	openCheckCacheParent = func(string) (checkCacheParent, error) { return parent, nil }
	t.Cleanup(func() { openCheckCacheParent = originalOpen })

	err := replaceCheckCacheFile(src, dst)
	var partial *checkCachePartialCommitError
	if !errors.As(err, &partial) || !errors.Is(err, wantErr) {
		t.Fatalf("replace error=%v, want partial commit wrapping sync error", err)
	}
	if !partial.Committed() {
		t.Fatal("partial commit error must report that rename committed")
	}
	if !parent.closed {
		t.Fatal("parent directory was not closed after sync failure")
	}
	got, readErr := os.ReadFile(dst)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("replacement not visible after partial commit: got %q want %q", got, want)
	}
}
