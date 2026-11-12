//go:build !windows
// +build !windows

package scriptrun

import (
	"os/exec"
	"syscall"
)

// processTree kills the worker and every descendant left in its process
// group. A descendant that deliberately creates a new session or process
// group escapes this cleanup; the local worker is trusted code, not a sandbox.
type processTree struct {
	pid int
}

func prepareProcessTree(command *exec.Cmd) {
	command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

func attachProcessTree(command *exec.Cmd) (*processTree, error) {
	return &processTree{pid: command.Process.Pid}, nil
}

func (tree *processTree) kill() {
	if tree == nil || tree.pid <= 0 {
		return
	}
	_ = syscall.Kill(-tree.pid, syscall.SIGKILL)
}

func (tree *processTree) close() {}
