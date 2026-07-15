package upgrade

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

const (
	InternalUpgradeHelperCommand  = "__upgrade-helper"
	InternalUpgradeCleanupCommand = "__upgrade-cleanup"
	InternalUpgradeEnvironment    = "VOLCENGINE_CLI_INTERNAL_UPGRADE"
)

// WindowsUpgradeLaunchOptions describes the parent-side helper launch.
type WindowsUpgradeLaunchOptions struct {
	CurrentExecutable string
	NewBinaryPath     string
	TargetPath        string
	WorkDir           string
	CurrentVersion    string
	ExpectedVersion   string
	ExplicitTarget    bool
	SkipSelfCheck     bool
	Stdout            io.Writer
	Stderr            io.Writer
}

// WindowsUpgradeHelperOptions describes the work performed after the parent exits.
type WindowsUpgradeHelperOptions struct {
	ParentPID       int
	NewBinaryPath   string
	TargetPath      string
	WorkDir         string
	CurrentVersion  string
	ExpectedVersion string
	ExplicitTarget  bool
	SkipSelfCheck   bool
	Stdout          io.Writer
	Stderr          io.Writer
}

var (
	waitForParentProcess    = waitForProcessExit
	replaceBinaryWithBackup = func(newPath, currentPath, expectedVersion string) error {
		return ReplaceBinaryWithBackup(newPath, currentPath, expectedVersion)
	}
	startWindowsUpgradeCommand = func(cmd *exec.Cmd) error {
		configureBackgroundCommand(cmd)
		return cmd.Start()
	}
	startWindowsUpgradeCleanup = launchWindowsUpgradeCleanup
	launchWindowsUpgradeHelper = LaunchWindowsUpgradeHelper
)

// LaunchWindowsUpgradeHelper copies the running CLI to a temporary executable
// and starts it without waiting. The copy remains runnable after the parent
// releases the target executable's Windows image lock.
func LaunchWindowsUpgradeHelper(opts WindowsUpgradeLaunchOptions) error {
	if strings.TrimSpace(opts.CurrentExecutable) == "" || strings.TrimSpace(opts.NewBinaryPath) == "" ||
		strings.TrimSpace(opts.TargetPath) == "" || strings.TrimSpace(opts.WorkDir) == "" {
		return fmt.Errorf("invalid Windows upgrade helper paths")
	}
	if strings.TrimSpace(opts.ExpectedVersion) == "" {
		return fmt.Errorf("missing expected version for Windows upgrade helper")
	}

	info, err := os.Stat(opts.CurrentExecutable)
	if err != nil {
		return fmt.Errorf("failed to stat current executable for helper: %v", err)
	}
	helperPath := filepath.Join(opts.WorkDir, "ve-upgrade-helper.exe")
	if err := copyFile(opts.CurrentExecutable, helperPath, info.Mode()); err != nil {
		return fmt.Errorf("failed to prepare Windows upgrade helper: %v", err)
	}

	args := []string{
		InternalUpgradeHelperCommand,
		"--parent-pid", strconv.Itoa(os.Getpid()),
		"--new-binary", opts.NewBinaryPath,
		"--target", opts.TargetPath,
		"--work-dir", opts.WorkDir,
		"--current-version", opts.CurrentVersion,
		"--expected-version", opts.ExpectedVersion,
	}
	if opts.ExplicitTarget {
		args = append(args, "--explicit-target")
	}
	if opts.SkipSelfCheck {
		args = append(args, "--skip-self-check")
	}

	cmd := exec.Command(helperPath, args...)
	cmd.Stdout = opts.Stdout
	cmd.Stderr = opts.Stderr
	cmd.Env = append(os.Environ(),
		InternalUpgradeEnvironment+"=1",
		EnvDisableUpdateCheck+"=1",
	)
	if err := startWindowsUpgradeCommand(cmd); err != nil {
		_ = os.Remove(helperPath)
		return fmt.Errorf("failed to start Windows upgrade helper: %v", err)
	}
	return nil
}

// RunWindowsUpgradeHelper waits for the parent to release the running binary,
// replaces it, performs the self-check/rollback, and reports the real outcome.
func RunWindowsUpgradeHelper(opts WindowsUpgradeHelperOptions) error {
	stdout := opts.Stdout
	if stdout == nil {
		stdout = os.Stdout
	}
	stderr := opts.Stderr
	if stderr == nil {
		stderr = os.Stderr
	}
	if opts.ParentPID <= 0 || strings.TrimSpace(opts.NewBinaryPath) == "" ||
		strings.TrimSpace(opts.TargetPath) == "" || strings.TrimSpace(opts.WorkDir) == "" {
		return fmt.Errorf("invalid Windows upgrade helper arguments")
	}
	if err := waitForParentProcess(opts.ParentPID); err != nil {
		return fmt.Errorf("failed waiting for parent process %d: %v", opts.ParentPID, err)
	}

	var replaceErr error
	if opts.SkipSelfCheck {
		replaceErr = ReplaceBinary(opts.NewBinaryPath, opts.TargetPath)
	} else {
		replaceErr = replaceBinaryWithBackup(opts.NewBinaryPath, opts.TargetPath, opts.ExpectedVersion)
	}

	cleanupErr := startWindowsUpgradeCleanup(opts.TargetPath, opts.WorkDir, os.Getpid())
	if cleanupErr != nil {
		fmt.Fprintf(stderr, "Warning: failed to schedule upgrade temporary-file cleanup: %v\n", cleanupErr)
	}
	if replaceErr != nil {
		return fmt.Errorf("installation failed: %v", replaceErr)
	}

	refreshVersionCacheAfterInstall(opts.ExplicitTarget, opts.ExpectedVersion)
	fmt.Fprintf(stdout, "\nSuccessfully upgraded Volcengine CLI from %s to %s!\n", opts.CurrentVersion, opts.ExpectedVersion)
	return nil
}

func launchWindowsUpgradeCleanup(targetPath, workDir string, parentPID int) error {
	cmd := exec.Command(targetPath,
		InternalUpgradeCleanupCommand,
		"--parent-pid", strconv.Itoa(parentPID),
		"--work-dir", workDir,
	)
	cmd.Env = append(os.Environ(),
		InternalUpgradeEnvironment+"=1",
		EnvDisableUpdateCheck+"=1",
	)
	configureBackgroundCommand(cmd)
	return cmd.Start()
}

// RunWindowsUpgradeCleanup waits for the helper executable to exit, then
// retries deletion because virus scanners can briefly retain Windows handles.
func RunWindowsUpgradeCleanup(parentPID int, workDir string) error {
	if parentPID > 0 {
		if err := waitForParentProcess(parentPID); err != nil {
			return fmt.Errorf("failed waiting for upgrade helper %d: %v", parentPID, err)
		}
	}
	workDir = filepath.Clean(workDir)
	if !strings.HasPrefix(filepath.Base(workDir), "ve-upgrade-") {
		return fmt.Errorf("refusing to remove unexpected upgrade work directory %q", workDir)
	}
	return removeUpgradeWorkDir(workDir)
}

func removeUpgradeWorkDir(workDir string) error {
	var lastErr error
	for attempt := 0; attempt < 20; attempt++ {
		lastErr = os.RemoveAll(workDir)
		if lastErr == nil {
			if _, err := os.Stat(workDir); os.IsNotExist(err) {
				return nil
			}
		}
		waitBeforeCleanupRetry()
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("directory still exists")
	}
	return fmt.Errorf("failed to remove upgrade work directory %q: %v", workDir, lastErr)
}

func shouldLaunchWindowsUpgradeHelper(goos, execPathOverride string) bool {
	return goos == "windows" && strings.TrimSpace(execPathOverride) == ""
}
