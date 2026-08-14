package upgrade

import (
	"errors"
	"os"
	"strings"
	"testing"
)

func TestIsUpgradeLockPermissionError(t *testing.T) {
	if !isUpgradeLockPermissionError(os.ErrPermission) {
		t.Fatal("os.ErrPermission should be treated as lock permission error")
	}
	if !isUpgradeLockPermissionError(errors.New("open foo: Access is denied.")) {
		t.Fatal("windows-style access denied should match")
	}
	if isUpgradeLockPermissionError(errors.New("file exists")) {
		t.Fatal("unrelated errors must not match")
	}
	if isUpgradeLockPermissionError(nil) {
		t.Fatal("nil error is not a permission error")
	}
}

func TestUpgradeLockOpenHint(t *testing.T) {
	lockPath := "/tmp/ve.upgrade.lock"
	hint := upgradeLockOpenHint(lockPath, os.ErrPermission)
	if hint == "" {
		t.Fatal("expected permission hint")
	}
	if !strings.Contains(hint, lockPath) {
		t.Fatalf("hint should mention lock path, got %q", hint)
	}
	if !strings.Contains(hint, "write access") || !strings.Contains(hint, "0600") || !strings.Contains(hint, "stale lock") {
		t.Fatalf("hint should mention directory write access, mode, and recovery, got %q", hint)
	}
	if upgradeLockOpenHint(lockPath, errors.New("busy")) != "" {
		t.Fatal("non-permission errors should not get a hint")
	}
}
