//go:build windows
// +build windows

package cmd

// Windows os.Rename does not reliably replace an existing destination. Reuse
// the bounded MoveFileExW(REPLACE_EXISTING|WRITE_THROUGH) implementation used
// by config transactions.
func replaceLoginCacheFilePlatform(src, dst string) error {
	return replaceFile(src, dst)
}
