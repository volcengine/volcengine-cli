//go:build windows
// +build windows

package cmd

import "os/exec"

func defaultStartDetachedProxyUpdateWorker(path string, args, env []string) error {
	process := exec.Command(path, args...)
	process.Env = env
	if err := process.Start(); err != nil {
		return err
	}
	return process.Process.Release()
}
