package cmd

import (
	"bytes"
	"strings"
	"testing"
)

func TestHelpUnknownCommandShowsRootUsage(t *testing.T) {
	ensureInitRootCmd()
	helpCmd, _, err := rootCmd.Find([]string{"help"})
	if err != nil {
		t.Fatalf("Find help command: %v", err)
	}

	var buf bytes.Buffer
	rootCmd.SetOut(&buf)
	rootCmd.SetErr(&buf)
	helpCmd.SetOut(&buf)
	helpCmd.SetErr(&buf)
	t.Cleanup(func() {
		// Restore shared root/help writers so later tests do not inherit this buffer.
		rootCmd.SetOut(nil)
		rootCmd.SetErr(nil)
		helpCmd.SetOut(nil)
		helpCmd.SetErr(nil)
	})

	runErr := helpCmd.RunE(helpCmd, []string{"definitely-not-a-command"})
	if runErr == nil {
		t.Fatal("expected error for unknown help target")
	}
	out := buf.String()
	if !strings.Contains(out, "Available Commands:") {
		t.Fatalf("expected root usage with available commands, got: %q", out)
	}
}
