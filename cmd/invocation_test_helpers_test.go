package cmd

import "testing"

func stubExecuteInvocation(t *testing.T, err error) *invocationParams {
	t.Helper()
	prev := executeInvocationHook
	captured := &invocationParams{}
	executeInvocationHook = func(ctx *Context, p invocationParams, buildInput func() (invocationInput, error)) error {
		*captured = p
		return err
	}
	t.Cleanup(func() { executeInvocationHook = prev })
	return captured
}
