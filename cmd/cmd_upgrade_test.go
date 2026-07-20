package cmd

import (
	"bytes"
	"strings"
	"testing"
)

func TestUpgradeCmdHasNoForceFlag(t *testing.T) {
	command := newUpgradeCmd()
	if command.Flags().Lookup("force") != nil {
		t.Fatal("ve upgrade must not expose --force")
	}
	if command.Flags().Lookup("yes") == nil {
		t.Fatal("expected --yes on ve upgrade")
	}
	if command.Flags().Lookup("version") == nil {
		t.Fatal("expected --version on ve upgrade")
	}
}

func TestUpgradeCmdRejectsForceFlag(t *testing.T) {
	command := newUpgradeCmd()
	var stdout, stderr bytes.Buffer
	command.SetOut(&stdout)
	command.SetErr(&stderr)
	command.SetArgs([]string{"--force"})

	err := command.Execute()
	if err == nil {
		t.Fatal("expected unknown flag error for ve upgrade --force")
	}
	msg := err.Error()
	if !strings.Contains(msg, "unknown flag") || !strings.Contains(msg, "force") {
		t.Fatalf("expected cobra unknown-flag error for --force, got: %v", err)
	}
	// Must fail at flag parse time; never enter DoUpgrade / network paths.
	out := stdout.String() + stderr.String()
	if strings.Contains(out, "Current version:") {
		t.Fatalf("upgrade body must not run when --force is passed:\n%s", out)
	}
}

func TestUpgradeCmdHelpOmitsForce(t *testing.T) {
	command := newUpgradeCmd()
	var buf bytes.Buffer
	command.SetOut(&buf)
	command.SetErr(&buf)
	command.SetArgs([]string{"--help"})
	if err := command.Execute(); err != nil {
		t.Fatalf("help: %v", err)
	}
	out := buf.String()
	if strings.Contains(out, "--force") {
		t.Fatalf("upgrade help must not mention --force:\n%s", out)
	}
	for _, want := range []string{"--yes", "--version", "Homebrew", "npm", "standalone"} {
		if !strings.Contains(out, want) {
			t.Fatalf("upgrade help missing %q:\n%s", want, out)
		}
	}
}
