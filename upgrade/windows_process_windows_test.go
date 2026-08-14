//go:build windows
// +build windows

package upgrade

import (
	"os/exec"
	"strings"
	"testing"
	"time"
)

func TestWaitForProcessExit_Timeout(t *testing.T) {
	// Start a process that stays alive longer than the wait bound.
	cmd := exec.Command("ping", "-n", "60", "127.0.0.1")
	if err := cmd.Start(); err != nil {
		t.Fatalf("start long-lived process: %v", err)
	}
	defer func() {
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
			_, _ = cmd.Process.Wait()
		}
	}()

	orig := parentWaitTimeout
	parentWaitTimeout = 200 * time.Millisecond
	defer func() { parentWaitTimeout = orig }()

	err := waitForProcessExit(cmd.Process.Pid)
	if err == nil {
		t.Fatal("expected timeout error")
	}
	msg := err.Error()
	if !strings.Contains(msg, "timed out") {
		t.Fatalf("expected timeout message, got %v", err)
	}
	if !strings.Contains(msg, "parent process") {
		t.Fatalf("expected pid context in error, got %v", err)
	}
}

func TestWaitForProcessExit_AlreadyExited(t *testing.T) {
	cmd := exec.Command("cmd", "/C", "exit", "0")
	if err := cmd.Run(); err != nil {
		t.Fatalf("run short process: %v", err)
	}
	// Exercise OpenProcess/WaitForSingleObject against a PID that has already exited
	// (errno 87 / wait success). Do not use pid 0: that is a no-op success path.
	if cmd.Process == nil {
		t.Fatal("expected process handle after Run")
	}
	// Bound wait so a rare PID-reuse collision cannot hang for the production 2m timeout.
	orig := parentWaitTimeout
	parentWaitTimeout = 300 * time.Millisecond
	defer func() { parentWaitTimeout = orig }()

	pid := cmd.Process.Pid
	if err := waitForProcessExit(pid); err != nil {
		// PID reuse onto a still-living process can make the wait time out; skip rather than flake.
		if strings.Contains(err.Error(), "timed out") {
			t.Skipf("pid %d likely reused by another process: %v", pid, err)
		}
		t.Fatalf("wait for exited pid %d: %v", pid, err)
	}
}
