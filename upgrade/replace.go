package upgrade

// Copyright 2022 Beijing Volcanoengine Technology Ltd.  All Rights Reserved.

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// selfCheckTimeout bounds `path version` so a hung new binary cannot block
// upgrade completion or rollback indefinitely. Overridable in tests.
var selfCheckTimeout = 15 * time.Second

var (
	osExecutable = os.Executable
	evalSymlinks = filepath.EvalSymlinks
	execCommand  = exec.Command
)

// ResolveExecPath returns the real path of the current CLI binary.
func ResolveExecPath() (string, error) {
	execPath, err := osExecutable()
	if err != nil {
		return "", fmt.Errorf("failed to get executable path: %v", err)
	}
	execPath, err = evalSymlinks(execPath)
	if err != nil {
		return "", fmt.Errorf("failed to resolve executable path: %v", err)
	}
	return execPath, nil
}

// ReplaceBinary atomically replaces currentPath with the file at newPath.
// On failure, the original binary is restored when possible.
func ReplaceBinary(newPath, currentPath string) error {
	info, err := os.Stat(currentPath)
	if err != nil {
		return fmt.Errorf("failed to stat current binary: %v", err)
	}
	perm := info.Mode()
	dir := filepath.Dir(currentPath)

	if runtime.GOOS == "windows" {
		// Windows locks a running executable image. DoUpgrade delegates replacement
		// of the live CLI to a copied helper and calls this only after the parent exits.
		oldPath := currentPath + ".old"
		_ = os.Remove(oldPath)

		if err := os.Rename(currentPath, oldPath); err != nil {
			return fmt.Errorf("failed to move current binary: %v; you may need elevated privileges", err)
		}
		if err := copyFile(newPath, currentPath, perm); err != nil {
			if rbErr := os.Rename(oldPath, currentPath); rbErr != nil {
				return fmt.Errorf("copy new binary failed: %v; rollback also failed (old binary kept at %s): %v", err, oldPath, rbErr)
			}
			return err
		}
		_ = os.Remove(oldPath)
		return nil
	}

	// Unique temp name in the same directory so concurrent upgrades (or a stale
	// leftover) cannot clobber each other; rename stays atomic on the same FS.
	tmpFile, err := os.CreateTemp(dir, ".ve.upgrade-*.tmp")
	if err != nil {
		return fmt.Errorf("failed to create temp file for upgrade: %v", err)
	}
	tmpPath := tmpFile.Name()
	if err := tmpFile.Close(); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("failed to close temp file for upgrade: %v", err)
	}
	if err := copyFile(newPath, tmpPath, perm); err != nil {
		_ = os.Remove(tmpPath)
		return err
	}
	if err := os.Rename(tmpPath, currentPath); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("failed to install new binary: %v; you may need to run with elevated privileges (sudo)", err)
	}
	return nil
}

// SelfCheckVersion runs `path version` and verifies the printed version matches expected.
// The child process is killed if it does not finish within selfCheckTimeout.
func SelfCheckVersion(path, expectedVersion string) error {
	expectedVersion = NormalizeVersion(expectedVersion)
	cmd := execCommand(path, "version")
	// Avoid nested background update checks (extra latency + stderr noise).
	cmd.Env = append(os.Environ(), EnvDisableUpdateCheck+"=1")

	var output strings.Builder
	cmd.Stdout = &output
	cmd.Stderr = &output
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("new binary self-check failed to start: %v", err)
	}

	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()

	var waitErr error
	select {
	case waitErr = <-done:
	case <-time.After(selfCheckTimeout):
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
		}
		<-done
		return fmt.Errorf("new binary self-check timed out after %s", selfCheckTimeout)
	}

	out := strings.TrimSpace(output.String())
	if waitErr != nil {
		return fmt.Errorf("new binary self-check failed: %v (%s)", waitErr, out)
	}
	// Take the first non-empty line only; ignore any trailing diagnostics.
	gotLine := firstNonEmptyLine(out)
	got := NormalizeVersion(gotLine)
	if got != expectedVersion {
		return fmt.Errorf("new binary version mismatch: expected %s, got %q", expectedVersion, gotLine)
	}
	return nil
}

func firstNonEmptyLine(s string) string {
	for _, line := range strings.Split(s, "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			return line
		}
	}
	return ""
}

// RollbackBinary restores backupPath over currentPath.
// Prefer rename (atomic replace on Unix). If that fails (common on Windows when
// the target exists), copy over the target without deleting it first so a failed
// rollback never leaves currentPath missing while the backup still exists.
func RollbackBinary(backupPath, currentPath string) error {
	if backupPath == "" {
		return fmt.Errorf("no backup path")
	}
	info, err := os.Stat(backupPath)
	if err != nil {
		return err
	}
	// Same-filesystem rename replaces the destination atomically on Unix.
	if err := os.Rename(backupPath, currentPath); err == nil {
		return nil
	}
	// Fallback: overwrite in place (O_TRUNC) so currentPath is never unlinked
	// before the restored content is written.
	if err := copyFile(backupPath, currentPath, info.Mode()); err != nil {
		return fmt.Errorf("rollback failed: %v (backup kept at %s)", err, backupPath)
	}
	_ = os.Remove(backupPath)
	return nil
}

// ReplaceBinaryWithBackup replaces current with newPath under an inter-process
// lock, keeping a unique same-directory backup until self-check succeeds.
// On self-check failure, restores the backup.
func ReplaceBinaryWithBackup(newPath, currentPath, expectedVersion string) error {
	lock, err := acquireUpgradeLock(currentPath)
	if err != nil {
		return err
	}
	defer lock.release()

	info, err := os.Stat(currentPath)
	if err != nil {
		return fmt.Errorf("failed to stat current binary: %v", err)
	}
	perm := info.Mode()
	backupFile, err := os.CreateTemp(filepath.Dir(currentPath), ".ve.backup-*.bak")
	if err != nil {
		return fmt.Errorf("failed to create backup file: %v", err)
	}
	backupPath := backupFile.Name()
	if err := backupFile.Close(); err != nil {
		_ = os.Remove(backupPath)
		return fmt.Errorf("failed to close backup file: %v", err)
	}

	// Keep a copy of the old binary for rollback after self-check.
	if err := copyFile(currentPath, backupPath, perm); err != nil {
		_ = os.Remove(backupPath)
		return fmt.Errorf("failed to backup current binary: %v", err)
	}

	if err := ReplaceBinary(newPath, currentPath); err != nil {
		_ = os.Remove(backupPath)
		return err
	}

	if err := SelfCheckVersion(currentPath, expectedVersion); err != nil {
		if rbErr := RollbackBinary(backupPath, currentPath); rbErr != nil {
			return fmt.Errorf("%v; rollback also failed: %v", err, rbErr)
		}
		return fmt.Errorf("%v; rolled back to previous version", err)
	}

	_ = os.Remove(backupPath)
	return nil
}

func copyFile(src, dst string, perm os.FileMode) error {
	source, err := os.Open(src)
	if err != nil {
		return err
	}
	defer source.Close()

	dest, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, perm)
	if err != nil {
		return err
	}
	defer dest.Close()

	if _, err := io.Copy(dest, source); err != nil {
		return err
	}
	if err := dest.Sync(); err != nil {
		return err
	}
	return dest.Chmod(perm)
}
