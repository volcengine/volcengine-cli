package cmd

import (
	"bytes"
	"strings"
	"testing"
)

func TestHelpUnknownCommandShowsRootUsage(t *testing.T) {
	initRootCmd()
	helpCmd, _, err := rootCmd.Find([]string{"help"})
	if err != nil {
		t.Fatalf("Find help command: %v", err)
	}

	var buf bytes.Buffer
	rootCmd.SetOut(&buf)
	rootCmd.SetErr(&buf)
	helpCmd.SetOut(&buf)
	helpCmd.SetErr(&buf)

	runErr := helpCmd.RunE(helpCmd, []string{"definitely-not-a-command"})
	if runErr == nil {
		t.Fatal("expected error for unknown help target")
	}
	out := buf.String()
	if !strings.Contains(out, "Available Commands:") {
		t.Fatalf("expected root usage with available commands, got: %q", out)
	}
}