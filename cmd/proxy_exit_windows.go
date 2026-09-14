//go:build windows
// +build windows

package cmd

import (
	"errors"
	"fmt"
	"io"
	"os/exec"
)

func defaultRunProxyExecutable(path string, args []string, stdin io.Reader, stdout, stderr io.Writer, env []string) error {
	process := exec.Command(path, args...)
	process.Stdin = stdin
	process.Stdout = stdout
	process.Stderr = stderr
	process.Env = env
	if err := process.Run(); err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			code := delegatedExitCode(exitErr)
			if code < 1 {
				code = 1
			}
			return &proxyProcessExitError{code: code}
		}
		return fmt.Errorf("run delegated CLI %s: %w", path, err)
	}
	return nil
}

func delegatedExitCode(err *exec.ExitError) int {
	return err.ExitCode()
}
