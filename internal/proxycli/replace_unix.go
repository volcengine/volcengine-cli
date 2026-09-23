//go:build !windows
// +build !windows

package proxycli

import "os"

func replaceFile(source, target string) error { return os.Rename(source, target) }
