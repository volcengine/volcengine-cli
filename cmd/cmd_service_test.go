package cmd

import (
	"bytes"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

func TestRunServiceCmdUnknownActionErrors(t *testing.T) {
	err := runServiceCmd(&cobra.Command{}, "sts", []string{"GetCallerIdentity"},
		[]string{"NonExistentAction", "---region", "cn-beijing"})
	if err == nil {
		t.Fatal("expected error for unknown action even with ---region present")
	}
	if !strings.Contains(err.Error(), "is not a supported action") {
		t.Fatalf("expected unsupported-action error, got: %v", err)
	}
	if !strings.Contains(err.Error(), "NonExistentAction") {
		t.Fatalf("expected error to name the action, got: %v", err)
	}
}

func TestRunServiceCmdValidActionReturnsNil(t *testing.T) {
	if err := runServiceCmd(&cobra.Command{}, "sts", []string{"GetCallerIdentity"},
		[]string{"GetCallerIdentity"}); err != nil {
		t.Fatalf("expected nil for valid action, got: %v", err)
	}
}

func TestRunServiceCmdNoActionShowsHelp(t *testing.T) {
	c := &cobra.Command{Use: "sts"}
	var b bytes.Buffer
	c.SetOut(&b)
	if err := runServiceCmd(c, "sts", []string{"GetCallerIdentity"}, nil); err != nil {
		t.Fatalf("expected help (nil error) when no action, got: %v", err)
	}
}

func TestRunServiceCmdHelpFlagShowsHelp(t *testing.T) {
	c := &cobra.Command{Use: "sts"}
	var b bytes.Buffer
	c.SetOut(&b)
	if err := runServiceCmd(c, "sts", []string{"GetCallerIdentity"},
		[]string{"NonExistentAction", "-h"}); err != nil {
		t.Fatalf("expected help (nil error) for -h, got: %v", err)
	}
}

func TestServiceValidActionRoutesToSubcommand(t *testing.T) {
	c, _, err := rootCmd.Find([]string{"sts", "GetCallerIdentity"})
	if err != nil {
		t.Fatalf("Find returned error: %v", err)
	}
	if c.Name() != "GetCallerIdentity" {
		t.Fatalf("expected routing to GetCallerIdentity subcommand, got %q", c.Name())
	}
}
