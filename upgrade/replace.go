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
)

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
		oldPath := currentPath + ".old"
		_ = os.Remove(oldPath)

		if err := os.Rename(currentPath, oldPath); err != nil {
			return fmt.Errorf("failed to move current binary: %v; you may need elevated privileges", err)
		}
		if err := copyFile(newPath, currentPath, perm); err != nil {
			_ = os.Rename(oldPath, currentPath)
			return err
		}
		_ = os.Remove(oldPath)
		return nil
	}

	tmpPath := filepath.Join(dir, ".ve.upgrade.tmp")
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
func SelfCheckVersion(path, expectedVersion string) error {
	expectedVersion = NormalizeVersion(expectedVersion)
	cmd := execCommand(path, "version")
	// Avoid nested background update checks (extra latency + stderr noise in CombinedOutput).
	cmd.Env = append(os.Environ(), EnvDisableUpdateCheck+"=1")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("new binary self-check failed: %v (%s)", err, strings.TrimSpace(string(out)))
	}
	// Take the first non-empty line only; ignore any trailing diagnostics.
	gotLine := firstNonEmptyLine(string(out))
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

// RollbackBinary restores backupPath over currentPath (best-effort).
func RollbackBinary(backupPath, currentPath string) error {
	if backupPath == "" {
		return fmt.Errorf("no backup path")
	}
	if _, err := os.Stat(backupPath); err != nil {
		return err
	}
	_ = os.Remove(currentPath)
	return os.Rename(backupPath, currentPath)
}

// ReplaceBinaryWithBackup replaces current with newPath, keeping a backup at current+".bak"
// until self-check succeeds. On self-check failure, restores the backup.
func ReplaceBinaryWithBackup(newPath, currentPath, expectedVersion string) error {
	info, err := os.Stat(currentPath)
	if err != nil {
		return fmt.Errorf("failed to stat current binary: %v", err)
	}
	perm := info.Mode()
	backupPath := currentPath + ".bak"
	_ = os.Remove(backupPath)

	// Keep a copy of the old binary for rollback after self-check.
	if err := copyFile(currentPath, backupPath, perm); err != nil {
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
	return dest.Chmod(perm)
}
