//go:build !windows
// +build !windows

package cmd

import "os"

// replaceFile atomically installs src at dst. src and dst are created in the
// same directory by WriteConfigToFile, so os.Rename provides an atomic replace
// on Unix filesystems.
func replaceFile(src, dst string) error {
	return os.Rename(src, dst)
}
