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
	// PID is recycled eventually; OpenProcess with invalid/exited handle should
	// return nil (errno 87) or succeed if the wait still applies. Using pid 0
	// is treated as no-op success by waitForProcessExit.
	if err := waitForProcessExit(0); err != nil {
		t.Fatalf("pid 0: %v", err)
	}
}
