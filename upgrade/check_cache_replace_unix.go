//go:build !windows
// +build !windows

package upgrade

import "os"

// replaceCheckCacheFile atomically installs src at dst. The cache writer
// creates src in the destination directory, so os.Rename is a same-filesystem
// atomic replacement on Unix.
func replaceCheckCacheFile(src, dst string) error {
	return os.Rename(src, dst)
}
