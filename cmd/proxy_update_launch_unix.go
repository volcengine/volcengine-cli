//go:build !windows
// +build !windows

package cmd

import (
	"os/exec"
	"syscall"
)

// defaultStartDetachedProxyUpdateWorker uses a short-lived shell as the
// direct child so it can be waited and reaped before ve replaces itself with
// the delegated CLI. The background worker is orphaned in a separate session,
// so it neither delays the exec handoff nor becomes the delegated CLI's zombie.
func defaultStartDetachedProxyUpdateWorker(path string, args, env []string) error {
	shellArgs := []string{"-c", `"$@" </dev/null >/dev/null 2>&1 &`, "ve-proxy-update", path}
	shellArgs = append(shellArgs, args...)
	process := exec.Command("/bin/sh", shellArgs...)
	process.Env = env
	process.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	return process.Run()
}
