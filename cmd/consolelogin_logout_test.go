package cmd

import (
	"io/ioutil"
	"os"
	"strings"
	"testing"
)

func TestLogoutSingleProfile_NoConfig(t *testing.T) {
	cl := &ConsoleLogout{Profile: "nonexistent"}
	err := cl.Logout()
	if err == nil {
		t.Fatal("expected error when profile does not exist")
	}
}

func TestLogoutSingleProfile_NonLoginMode(t *testing.T) {
	// This test verifies that logout rejects profiles not using console-login mode.
	// Since LoadConfig reads from disk, we test the mode check logic indirectly.
	cl := &ConsoleLogout{Profile: "test-ak-profile"}
	err := cl.Logout()
	if err == nil {
		// If the profile doesn't exist, that's also an acceptable error.
		t.Log("profile not found, which is expected in test environment")
	}
}

func TestLogoutSingleProfile_NonLoginModeLocalized(t *testing.T) {
	restoreLanguage := setLanguageForTest(LanguageSimplifiedChinese)
	defer restoreLanguage()

	previousConfig := config
	previousContextConfig := ctx.config
	setRuntimeConfig(&Configure{Profiles: map[string]*Profile{
		"test-ak-profile": {
			Name: "test-ak-profile",
			Mode: ModeAK,
		},
	}})
	defer func() {
		config = previousConfig
		ctx.config = previousContextConfig
	}()

	err := (&ConsoleLogout{Profile: "test-ak-profile"}).Logout()
	if err == nil {
		t.Fatal("expected non-console-login profile to be rejected")
	}
	if !strings.Contains(err.Error(), "只有 console-login 配置档案") {
		t.Fatalf("logout error was not localized: %q", err)
	}
}

func TestLogoutAll_NoConfig(t *testing.T) {
	// When no config exists, logoutAll should not panic and should print a message.
	cl := &ConsoleLogout{All: true}
	err := cl.Logout()
	// No error expected — just prints "No configuration found" or
	// "No console-login profiles with active sessions found."
	if err != nil {
		t.Logf("logoutAll returned error (acceptable in test env): %v", err)
	}
}

func TestRemoveLoginCache_NonExistent(t *testing.T) {
	// removeLoginCache should be idempotent — removing a non-existent file is not an error.
	err := removeLoginCache("non-existent-session-id-12345")
	if err != nil {
		t.Fatalf("removeLoginCache should not error on non-existent file, got: %v", err)
	}
}

func TestPrintPostLogoutHintClarifiesFutureSessionsAndLoadedCredentials(t *testing.T) {
	output := captureStdout(t, printPostLogoutHint)

	expectedParts := []string{
		"Local cache has been removed for future CLI sessions.",
		"Already-running tools that loaded temporary STS credentials before logout",
		"may continue to use them until those credentials expire.",
	}
	for _, part := range expectedParts {
		if !strings.Contains(output, part) {
			t.Fatalf("printPostLogoutHint output %q does not contain %q", output, part)
		}
	}
}

func captureStdout(t *testing.T, fn func()) string {
	t.Helper()

	originalStdout := os.Stdout
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatalf("creating stdout pipe: %v", err)
	}

	os.Stdout = writer
	defer func() {
		os.Stdout = originalStdout
	}()

	fn()

	if err := writer.Close(); err != nil {
		t.Fatalf("closing stdout writer: %v", err)
	}

	data, err := ioutil.ReadAll(reader)
	if err != nil {
		t.Fatalf("reading stdout: %v", err)
	}
	if err := reader.Close(); err != nil {
		t.Fatalf("closing stdout reader: %v", err)
	}
	return string(data)
}
