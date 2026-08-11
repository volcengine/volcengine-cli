package upgrade

// Copyright 2022 Beijing Volcanoengine Technology Ltd.  All Rights Reserved.

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type upgradeLock struct {
	file *os.File
}

func acquireUpgradeLock(currentPath string) (*upgradeLock, error) {
	lockPath := currentPath + ".upgrade.lock"
	// 0600: only the creating user can re-open the lock next to the binary.
	// That matches typical single-user installs; multi-user permission failures
	// get a clearer hint on OpenFile only (not on tryLock — busy can look like EACCES).
	file, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, fmt.Errorf("failed to open upgrade lock %s: %v%s", lockPath, err, upgradeLockOpenHint(lockPath, err))
	}
	if err := tryLockUpgradeFile(file); err != nil {
		_ = file.Close()
		if isUpgradeLockBusy(err) {
			return nil, fmt.Errorf("another upgrade is already in progress for %s (lock %s)", currentPath, lockPath)
		}
		// Do not attach ownership/stale-lock hint here: after a successful open,
		// failures are lock contention or OS-specific flock errors, not 0600 ownership.
		return nil, fmt.Errorf("failed to lock upgrade target %s: %v", currentPath, err)
	}
	return &upgradeLock{file: file}, nil
}

// acquireExclusiveFileLock opens lockPath and takes a non-blocking exclusive OS
// file lock (used by notice-cache timed wait). Callers that need to wait must
// poll; unbounded blocking flock is intentionally not exposed here.
//
// Note: the returned *upgradeLock is a small shared file-lock holder type name
// (historical); this helper is not the binary-upgrade lock path and must not
// reuse upgrade-only recovery wording (do not tell users to delete this lock
// as a "stale upgrade lock").
func acquireExclusiveFileLock(lockPath string) (*upgradeLock, error) {
	if err := os.MkdirAll(filepath.Dir(lockPath), 0700); err != nil {
		return nil, err
	}
	if err := rejectNonRegularPath(lockPath); err != nil {
		return nil, err
	}
	file, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		// Generic permission hint only — do not suggest removing the lock file
		// (unlinking a held notice lock enables dual exclusive holders).
		hint := ""
		if isUpgradeLockPermissionError(err) {
			hint = "; check write access to the lock directory and ownership of " + lockPath + " (mode 0600)"
		}
		return nil, fmt.Errorf("failed to open lock %s: %v%s", lockPath, err, hint)
	}
	if err := tryLockUpgradeFile(file); err != nil {
		_ = file.Close()
		return nil, err
	}
	return &upgradeLock{file: file}, nil
}

// rejectNonRegularPath fails if path exists and is not a plain file (symlink,
// directory, FIFO, etc.). Missing path is OK (caller may create it).
func rejectNonRegularPath(path string) error {
	info, err := os.Lstat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("%s is not a regular file (mode %s)", path, info.Mode())
	}
	return nil
}

// upgradeLockOpenHint appends recovery guidance only for OpenFile permission errors
// (cannot create/open the per-binary lock). Hint covers directory write access and
// ownership of an existing 0600 lock left by another user.
func upgradeLockOpenHint(lockPath string, err error) string {
	if err == nil || !isUpgradeLockPermissionError(err) {
		return ""
	}
	return fmt.Sprintf(
		"; check write access to the lock directory and ownership of %s (mode 0600) — if no upgrade is running, fix ownership or remove the stale lock and retry",
		lockPath,
	)
}

func isUpgradeLockPermissionError(err error) bool {
	if err == nil {
		return false
	}
	if os.IsPermission(err) {
		return true
	}
	// Windows and some wrapped errors surface access denied without os.IsPermission.
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "permission denied") ||
		strings.Contains(msg, "access is denied") ||
		strings.Contains(msg, "access denied")
}

func (l *upgradeLock) release() {
	if l == nil || l.file == nil {
		return
	}
	_ = unlockUpgradeFile(l.file)
	_ = l.file.Close()
}
