//go:build !windows
// +build !windows

package cmd

import (
	"fmt"
	"os"
	"path/filepath"
)

var openReplaceFileParent = func(path string) (replaceFileParent, error) {
	return os.Open(path)
}

type replaceFileParent interface {
	Sync() error
	Close() error
}

// replaceFile atomically installs src at dst. src and dst are created in the
// same directory by WriteConfigToFile, so os.Rename provides an atomic replace
// on Unix filesystems.
func replaceFile(src, dst string) error {
	if err := os.Rename(src, dst); err != nil {
		return err
	}

	// The temporary file contents are synced before replaceFile is called.
	// Sync the containing directory as well so the renamed directory entry is
	// durable across a crash. A sync failure is reported, but only after the
	// directory handle has been closed.
	dir, err := openReplaceFileParent(filepath.Dir(dst))
	if err != nil {
		return &PartialCommitError{Err: fmt.Errorf("open parent directory after replacing file: %w", err)}
	}
	syncErr := dir.Sync()
	closeErr := dir.Close()
	if syncErr != nil {
		return &PartialCommitError{Err: fmt.Errorf("sync parent directory after replacing file: %w", syncErr)}
	}
	if closeErr != nil {
		return &PartialCommitError{Err: fmt.Errorf("close parent directory after replacing file: %w", closeErr)}
	}
	return nil
}
