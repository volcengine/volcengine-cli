//go:build !windows
// +build !windows

package cmd

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

type stubReplaceFileParent struct {
	syncErr  error
	closeErr error
	synced   bool
	closed   bool
}

func (d *stubReplaceFileParent) Sync() error {
	d.synced = true
	return d.syncErr
}

func (d *stubReplaceFileParent) Close() error {
	d.closed = true
	return d.closeErr
}

func TestReplaceFileUnixSyncsParentDirectory(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "replacement")
	dst := filepath.Join(dir, "config.json")
	want := []byte("new config")
	if err := os.WriteFile(src, want, 0600); err != nil {
		t.Fatal(err)
	}

	originalOpen := openReplaceFileParent
	parent := &stubReplaceFileParent{}
	var openedPath string
	openReplaceFileParent = func(path string) (replaceFileParent, error) {
		openedPath = path
		return parent, nil
	}
	t.Cleanup(func() { openReplaceFileParent = originalOpen })

	if err := replaceFile(src, dst); err != nil {
		t.Fatalf("replaceFile: %v", err)
	}
	if openedPath != dir {
		t.Fatalf("opened parent = %q, want %q", openedPath, dir)
	}
	if !parent.synced || !parent.closed {
		t.Fatalf("parent lifecycle: synced=%v closed=%v", parent.synced, parent.closed)
	}
	got, err := os.ReadFile(dst)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("destination content = %q, want %q", got, want)
	}
}

func TestReplaceFileUnixReportsParentSyncErrorAfterRename(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "replacement")
	dst := filepath.Join(dir, "config.json")
	want := []byte("new config")
	if err := os.WriteFile(src, want, 0600); err != nil {
		t.Fatal(err)
	}

	wantErr := errors.New("injected directory sync failure")
	originalOpen := openReplaceFileParent
	parent := &stubReplaceFileParent{syncErr: wantErr}
	openReplaceFileParent = func(string) (replaceFileParent, error) {
		return parent, nil
	}
	t.Cleanup(func() { openReplaceFileParent = originalOpen })

	if err := replaceFile(src, dst); !errors.Is(err, wantErr) {
		t.Fatalf("replaceFile error = %v, want sync error", err)
	}
	if !parent.closed {
		t.Fatal("parent directory was not closed after sync failure")
	}
	got, err := os.ReadFile(dst)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("rename did not complete before sync failure: got %q, want %q", got, want)
	}
}
