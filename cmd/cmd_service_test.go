package cmd

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

var errStubInvocation = errors.New("stub invocation")

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

func TestRunServiceCmdUnknownActionWithForceBypassesUnsupportedError(t *testing.T) {
	captured := stubExecuteInvocation(t, errStubInvocation)
	err := runServiceCmd(&cobra.Command{}, "sts", []string{"GetCallerIdentity"},
		[]string{"NonExistentAction", "---version", "2024-01-01", "---force", "---region", "cn-beijing"})
	if !errors.Is(err, errStubInvocation) {
		t.Fatalf("expected stub invocation error, got: %v", err)
	}
	if strings.Contains(err.Error(), "is not a supported action") {
		t.Fatalf("---force should bypass unsupported action error, got: %v", err)
	}
	if captured.action != "NonExistentAction" || captured.version != "2024-01-01" {
		t.Fatalf("unexpected invocation params: action=%q version=%q", captured.action, captured.version)
	}
}

func TestRunServiceCmdForceUsesPositionalActionNotVersionValue(t *testing.T) {
	// Fixed flags before action: version value must not be mistaken for the action name.
	captured := stubExecuteInvocation(t, errStubInvocation)
	err := runServiceCmd(&cobra.Command{}, "sts", []string{"GetCallerIdentity"},
		[]string{"---version", "2024-01-01", "---force", "UnknownAction"})
	if !errors.Is(err, errStubInvocation) {
		t.Fatalf("expected stub invocation error, got: %v", err)
	}
	if captured.action != "UnknownAction" {
		t.Fatalf("action = %q, want %q", captured.action, "UnknownAction")
	}
	if captured.version != "2024-01-01" {
		t.Fatalf("version = %q, want %q", captured.version, "2024-01-01")
	}
}

func TestRunServiceCmdValidActionDispatchesWhenCobraMissedSubcommand(t *testing.T) {
	captured := stubExecuteInvocation(t, errStubInvocation)
	err := runServiceCmd(&cobra.Command{}, "sts", []string{"GetCallerIdentity"},
		[]string{"GetCallerIdentity"})
	if !errors.Is(err, errStubInvocation) {
		t.Fatalf("expected stub invocation error, got: %v", err)
	}
	if captured.action != "GetCallerIdentity" {
		t.Fatalf("action = %q, want %q", captured.action, "GetCallerIdentity")
	}
}

func TestRunServiceCmdValidActionDispatchesWhenFlagsBeforeAction(t *testing.T) {
	captured := stubExecuteInvocation(t, errStubInvocation)
	err := runServiceCmd(&cobra.Command{}, "sts", []string{"GetCallerIdentity"},
		[]string{"---region", "cn-beijing", "GetCallerIdentity"})
	if !errors.Is(err, errStubInvocation) {
		t.Fatalf("expected stub invocation error, got: %v", err)
	}
	if captured.action != "GetCallerIdentity" {
		t.Fatalf("action = %q, want %q", captured.action, "GetCallerIdentity")
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

func TestRunServiceCmdDoesNotTreatDashHValueAsHelp(t *testing.T) {
	c := &cobra.Command{Use: "sts"}
	var b bytes.Buffer
	c.SetOut(&b)

	err := runServiceCmd(c, "sts", []string{"GetCallerIdentity"},
		[]string{"ReadmeUnknownAction", "--Description", "-h"})
	if err == nil {
		t.Fatal("expected unsupported action error, got nil help result")
	}
	if !strings.Contains(err.Error(), "is not a supported action") {
		t.Fatalf("expected unsupported action error, got: %v", err)
	}
	if b.Len() != 0 {
		t.Fatalf("business value -h must not render service help:\n%s", b.String())
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
