//go:build !windows
// +build !windows

package cmd

import (
	"errors"
	"io"
	"reflect"
	"testing"
)

func TestDefaultRunProxyExecutableReplacesCurrentProcess(t *testing.T) {
	old := replaceProxyProcess
	defer func() { replaceProxyProcess = old }()
	wantErr := errors.New("exec stopped for test")
	var gotPath string
	var gotArgv, gotEnv []string
	replaceProxyProcess = func(path string, argv, env []string) error {
		gotPath = path
		gotArgv = append([]string(nil), argv...)
		gotEnv = append([]string(nil), env...)
		return wantErr
	}

	err := defaultRunProxyExecutable(
		"/managed/arkcli",
		[]string{"models", "list", "--profile", "child"},
		nil,
		io.Discard,
		io.Discard,
		[]string{"TEST_ENV=1"},
	)
	if !errors.Is(err, wantErr) {
		t.Fatalf("defaultRunProxyExecutable() error = %v", err)
	}
	if gotPath != "/managed/arkcli" {
		t.Fatalf("path = %q", gotPath)
	}
	if want := []string{"/managed/arkcli", "models", "list", "--profile", "child"}; !reflect.DeepEqual(gotArgv, want) {
		t.Fatalf("argv = %q, want %q", gotArgv, want)
	}
	if !reflect.DeepEqual(gotEnv, []string{"TEST_ENV=1"}) {
		t.Fatalf("env = %q", gotEnv)
	}
}
