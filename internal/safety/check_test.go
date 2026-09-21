package safety

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestIsInteractiveRejectsNonTerminals(t *testing.T) {
	devNull, err := os.Open(os.DevNull)
	if err != nil {
		t.Fatal(err)
	}
	defer devNull.Close()
	file, err := os.Create(filepath.Join(t.TempDir(), "input"))
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	defer writer.Close()
	for name, input := range map[string]io.Reader{
		"null device": devNull,
		"file":        file,
		"pipe":        reader,
		"buffer":      strings.NewReader("yes\n"),
		"nil":         nil,
	} {
		t.Run(name, func(t *testing.T) {
			if IsInteractive(input) {
				t.Fatal("non-terminal input reported as interactive")
			}
		})
	}
}

func TestIsInteractiveControllingTerminal(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("/dev/tty is a Unix device")
	}
	terminal, err := os.Open("/dev/tty")
	if err != nil {
		t.Skipf("no controlling terminal: %v", err)
	}
	defer terminal.Close()
	if !IsInteractive(terminal) {
		t.Fatal("controlling terminal reported as non-interactive")
	}
}

func TestCheckAndConfirmDenyCannotBeSkipped(t *testing.T) {
	policy := &Policy{
		Enabled: true,
		Rules:   []Rule{{Pattern: "ecs:Delete*", Action: ActionDeny}},
	}
	err := CheckAndConfirm(strings.NewReader("yes\n"), &bytes.Buffer{}, true, policy,
		CommandInfo{Product: "ecs", Action: "DeleteInstance"}, true, "")
	if err == nil || !strings.Contains(err.Error(), "blocked by safety policy") {
		t.Fatalf("deny error = %v", err)
	}
}

func TestCheckAndConfirmInteractiveApproval(t *testing.T) {
	policy := &Policy{
		Enabled: true,
		Rules:   []Rule{{Pattern: "ecs:Update*", Action: ActionConfirm}},
	}
	command := CommandInfo{Product: "ecs", Action: "UpdateInstance"}

	var output bytes.Buffer
	if err := CheckAndConfirm(strings.NewReader(" YeS \n"), &output, true, policy, command, false, ""); err != nil {
		t.Fatalf("interactive yes returned error: %v", err)
	}
	if !strings.Contains(output.String(), "UpdateInstance") {
		t.Fatalf("prompt = %q, want command", output.String())
	}

	err := CheckAndConfirm(strings.NewReader("no\n"), &bytes.Buffer{}, true, policy, command, false, "")
	if err == nil || !strings.Contains(err.Error(), "cancelled") {
		t.Fatalf("interactive no error = %v", err)
	}
}

func TestCheckAndConfirmNonInteractiveRequiresRetryApproval(t *testing.T) {
	policy := &Policy{
		Enabled: true,
		Rules:   []Rule{{Pattern: "ecs:Update*", Action: ActionConfirm}},
	}
	command := CommandInfo{Product: "ecs", Action: "UpdateInstance"}

	err := CheckAndConfirm(strings.NewReader(""), &bytes.Buffer{}, false, policy, command, false, "re-run the same command with --yes")
	if err == nil || !strings.Contains(err.Error(), "--yes") {
		t.Fatalf("non-interactive error = %v", err)
	}

	if err := CheckAndConfirm(strings.NewReader(""), &bytes.Buffer{}, false, policy, command, true, ""); err != nil {
		t.Fatalf("explicit approval returned error: %v", err)
	}
}

func TestEnvironmentSkipsOnlyConfirmation(t *testing.T) {
	for _, value := range []string{"1", "true", "TRUE", "yes", "on"} {
		t.Run(value, func(t *testing.T) {
			t.Setenv(EnvSkipConfirm, value)
			if !SkipConfirmFromEnvironment() {
				t.Fatalf("SkipConfirmFromEnvironment() = false for %q", value)
			}
		})
	}
}

func TestConfirmationNoticeRequiresFreshSingleUseApproval(t *testing.T) {
	policy := &Policy{Enabled: true, Rules: []Rule{{Pattern: "*:*", Action: ActionConfirm}}}
	for _, retry := range []string{"re-run the same command with --yes", "call the same MCP tool again with yes=true"} {
		err := CheckAndConfirm(nil, nil, false, policy, CommandInfo{Product: "sts", Action: "GetCallerIdentity"}, false, retry)
		if err == nil {
			t.Fatal("unapproved invocation must be blocked")
		}
		for _, want := range []string{"STOP:", "Briefly explain", "end this turn and wait", "NEW user reply", "Only after approval", "internally", "ONCE", "unchanged arguments", "do not ask the user to handle this flag", "even on failure", "Start every new operation without --yes/yes=true", "explain and ask only if blocked", "Never reuse earlier approvals", retry} {
			if !strings.Contains(err.Error(), want) {
				t.Errorf("confirmation notice missing %q: %s", want, err)
			}
		}
		if strings.Contains(err.Error(), "Explain this exact command, environment, target and parameters") {
			t.Error("notice must not require a full command walkthrough")
		}
	}
}
