//go:build windows
// +build windows

package cmd

// The stdio cancellation test is skipped on Windows before this is used.
func scriptProcessAlive(int) bool { return false }
