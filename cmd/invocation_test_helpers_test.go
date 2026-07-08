package cmd

import "testing"

func stubExecuteInvocation(t *testing.T, err error) {
	t.Helper()
	prev := executeInvocationHook
	executeInvocationHook = func(ctx *Context, p invocationParams, buildInput func() (invocationInput, error)) error {
		return err
	}
	t.Cleanup(func() { executeInvocationHook = prev })
}