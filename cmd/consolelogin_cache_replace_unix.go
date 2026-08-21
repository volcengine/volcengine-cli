//go:build !windows
// +build !windows

package cmd

import "os"

func replaceLoginCacheFilePlatform(src, dst string) error {
	return os.Rename(src, dst)
}
