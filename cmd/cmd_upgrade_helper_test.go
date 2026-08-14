package cmd

import (
	"testing"

	"github.com/volcengine/volcengine-cli/upgrade"
)

func TestUpgradeHelperCommandIsHiddenAndParsesOptions(t *testing.T) {
	t.Setenv(upgrade.InternalUpgradeEnvironment, "1")
	originalRun := runWindowsUpgradeHelper
	defer func() { runWindowsUpgradeHelper = originalRun }()

	var captured upgrade.WindowsUpgradeHelperOptions
	runWindowsUpgradeHelper = func(opts upgrade.WindowsUpgradeHelperOptions) error {
		captured = opts
		return nil
	}
	command := newUpgradeHelperCmd()
	if !command.Hidden {
		t.Fatal("internal upgrade helper must be hidden")
	}
	command.SetArgs([]string{
		"--parent-pid", "1234",
		"--new-binary", "new.exe",
		"--target", "ve.exe",
		"--work-dir", "work",
		"--current-version", "1.0.0",
		"--expected-version", "2.0.0",
		"--explicit-target",
	})
	if err := command.Execute(); err != nil {
		t.Fatal(err)
	}
	if captured.ParentPID != 1234 || captured.NewBinaryPath != "new.exe" || captured.TargetPath != "ve.exe" {
		t.Fatalf("unexpected helper options: %+v", captured)
	}
	if !captured.ExplicitTarget || captured.CurrentVersion != "1.0.0" || captured.ExpectedVersion != "2.0.0" {
		t.Fatalf("missing helper version options: %+v", captured)
	}
}

func TestUpgradeHelperCommandRejectsExternalInvocation(t *testing.T) {
	t.Setenv(upgrade.InternalUpgradeEnvironment, "")
	command := newUpgradeHelperCmd()
	command.SetArgs([]string{
		"--parent-pid", "1234",
		"--new-binary", "new.exe",
		"--target", "ve.exe",
		"--work-dir", "work",
		"--expected-version", "2.0.0",
	})
	if err := command.Execute(); err == nil {
		t.Fatal("expected internal helper command to reject direct invocation")
	}
}

func TestUpgradeCleanupCommandIsHiddenAndParsesOptions(t *testing.T) {
	t.Setenv(upgrade.InternalUpgradeEnvironment, "1")
	originalRun := runWindowsUpgradeCleanup
	defer func() { runWindowsUpgradeCleanup = originalRun }()

	var gotPID int
	var gotWorkDir string
	runWindowsUpgradeCleanup = func(parentPID int, workDir string) error {
		gotPID = parentPID
		gotWorkDir = workDir
		return nil
	}
	command := newUpgradeCleanupCmd()
	if !command.Hidden {
		t.Fatal("internal upgrade cleanup must be hidden")
	}
	command.SetArgs([]string{"--parent-pid", "4321", "--work-dir", "ve-upgrade-work"})
	if err := command.Execute(); err != nil {
		t.Fatal(err)
	}
	if gotPID != 4321 || gotWorkDir != "ve-upgrade-work" {
		t.Fatalf("cleanup arguments = %d %q", gotPID, gotWorkDir)
	}
}
