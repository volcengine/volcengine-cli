package cmd

import (
	"fmt"
	"os"
	"path/filepath"
)

// acquireCredentialCacheLock serializes replacement and logout of one cache
// file. The stable sibling lock file is deliberately retained so concurrent
// processes can never lock different inodes for the same cache path.
func acquireCredentialCacheLock(cachePath string) (*configFileLock, error) {
	dir := filepath.Dir(cachePath)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, fmt.Errorf("create credential cache directory: %w", err)
	}
	if err := os.Chmod(dir, 0700); err != nil {
		return nil, fmt.Errorf("set credential cache directory permissions: %w", err)
	}
	lock, err := acquireConfigFileLock(cachePath + ".lock")
	if err != nil {
		return nil, fmt.Errorf("lock credential cache: %w", err)
	}
	return lock, nil
}
