//go:build !windows
// +build !windows

package cmd

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/volcengine/volcengine-cli/internal/proxycli"
)

func TestDefaultLaunchProxyUpdateCheckDoesNotWaitForWorker(t *testing.T) {
	oldExecutable := proxyExecutablePath
	defer func() { proxyExecutablePath = oldExecutable }()

	directory := t.TempDir()
	release := filepath.Join(directory, "release-worker")
	marker := filepath.Join(directory, "worker-finished")
	script := filepath.Join(directory, "slow-ve")
	if err := os.WriteFile(script, []byte("#!/bin/sh\nwhile [ ! -f \"$PROXY_HELPER_RELEASE\" ]; do sleep 1; done\nprintf done > \"$PROXY_HELPER_MARKER\"\n"), 0755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PROXY_HELPER_RELEASE", release)
	t.Setenv("PROXY_HELPER_MARKER", marker)
	proxyExecutablePath = func() (string, error) { return script, nil }

	launchDone := make(chan error, 1)
	go func() {
		launchDone <- defaultLaunchProxyUpdateCheck(proxycli.ToolArkCLI, "1.2.3")
	}()
	select {
	case err := <-launchDone:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		if err := os.WriteFile(release, nil, 0600); err != nil {
			t.Fatal(err)
		}
		if err := <-launchDone; err != nil {
			t.Fatal(err)
		}
		t.Fatal("defaultLaunchProxyUpdateCheck() waited for the worker")
	}
	if err := os.WriteFile(release, nil, 0600); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(3 * time.Second)
	for {
		if data, err := os.ReadFile(marker); err == nil {
			if string(data) != "done" {
				t.Fatalf("worker marker = %q", data)
			}
			break
		} else if !os.IsNotExist(err) {
			t.Fatal(err)
		}
		if time.Now().After(deadline) {
			t.Fatal("detached update worker did not finish")
		}
		time.Sleep(10 * time.Millisecond)
	}
}
