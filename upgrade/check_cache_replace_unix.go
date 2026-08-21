//go:build !windows
// +build !windows

package upgrade

import (
	"fmt"
	"os"
	"path/filepath"
)

type checkCacheParent interface {
	Sync() error
	Close() error
}

var openCheckCacheParent = func(path string) (checkCacheParent, error) {
	return os.Open(path)
}

// checkCachePartialCommitError means dst already names the new cache, but its
// directory entry could not be confirmed durable. Retrying as if rename had not
// happened could overwrite a newer concurrent cache value.
type checkCachePartialCommitError struct {
	err error
}

func (e *checkCachePartialCommitError) Error() string {
	return "cache replacement committed but durability is uncertain: " + e.err.Error()
}

func (e *checkCachePartialCommitError) Unwrap() error { return e.err }

func (e *checkCachePartialCommitError) Committed() bool { return e != nil }

// replaceCheckCacheFile atomically installs src at dst. The cache writer
// creates src in the destination directory, so os.Rename is a same-filesystem
// atomic replacement on Unix.
func replaceCheckCacheFile(src, dst string) error {
	if err := os.Rename(src, dst); err != nil {
		return err
	}
	parent, err := openCheckCacheParent(filepath.Dir(dst))
	if err != nil {
		return &checkCachePartialCommitError{err: fmt.Errorf("open parent directory: %w", err)}
	}
	syncErr := parent.Sync()
	closeErr := parent.Close()
	if syncErr != nil {
		return &checkCachePartialCommitError{err: fmt.Errorf("sync parent directory: %w", syncErr)}
	}
	if closeErr != nil {
		return &checkCachePartialCommitError{err: fmt.Errorf("close parent directory: %w", closeErr)}
	}
	return nil
}
