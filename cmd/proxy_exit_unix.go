//go:build !windows
// +build !windows

package cmd

import (
	"io"
	"syscall"
)

var replaceProxyProcess = syscall.Exec

func defaultRunProxyExecutable(path string, args []string, _ io.Reader, _, _ io.Writer, env []string) error {
	argv := make([]string, 0, len(args)+1)
	argv = append(argv, path)
	argv = append(argv, args...)
	return replaceProxyProcess(path, argv, env)
}
