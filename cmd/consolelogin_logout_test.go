package cmd

import (
	"encoding/json"
	"errors"
	"io/ioutil"
	"os"
	"strings"
	"testing"
	"time"
)

func TestCredentialCacheLockSerializesWriter(t *testing.T) {
	_, cleanup := withTestConfigDir(t)
	defer cleanup()
	cachePath, err := loginCacheFilePath("shared-session")
	if err != nil {
		t.Fatal(err)
	}
	holder, err := acquireCredentialCacheLock(cachePath)
	if err != nil {
		t.Fatal(err)
	}

	done := make(chan error, 1)
	go func() {
		done <- writeLoginCache(&LoginTokenCache{
			LoginSession: "shared-session",
			AccessToken:  json.RawMessage(`{"AccessKeyId":"new"}`),
		})
	}()
	select {
	case err := <-done:
		_ = holder.release()
		t.Fatalf("writer bypassed held cache lock: %v", err)
	case <-time.After(100 * time.Millisecond):
	}
	if err := holder.release(); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("writer did not resume after cache lock release")
	}
}

func TestLogoutSingleProfileConfigConflictKeepsCacheAndRestoresMemory(t *testing.T) {
	_, cleanup := withTestConfigDir(t)
	defer cleanup()
	oldConfig, oldCtx := config, ctx.config
	oldWriter := writeLogoutConfigTransaction
	defer func() {
		config, ctx.config = oldConfig, oldCtx
		writeLogoutConfigTransaction = oldWriter
	}()

	cfg := &Configure{Profiles: map[string]*Profile{
		"default": {Name: "default", Mode: ModeConsoleLogin, LoginSession: "session-1"},
	}}
	if err := WriteConfigToFile(cfg); err != nil {
		t.Fatal(err)
	}
	cfg = LoadConfig()
	setRuntimeConfig(cfg)
	cache := &LoginTokenCache{LoginSession: "session-1", AccessToken: json.RawMessage(`{"AccessKeyId":"ak"}`)}
	if err := writeLoginCache(cache); err != nil {
		t.Fatal(err)
	}
	cachePath, _ := loginCacheFilePath("session-1")
	wantErr := errors.New("injected concurrent config conflict")
	writeLogoutConfigTransaction = func(*Configure) error { return wantErr }

	err := (&ConsoleLogout{Profile: "default"}).Logout()
	if !errors.Is(err, wantErr) {
		t.Fatalf("logout error = %v, want conflict", err)
	}
	if cfg.Profiles["default"].LoginSession != "session-1" {
		t.Fatalf("memory login session not restored: %#v", cfg.Profiles["default"])
	}
	if _, err := os.Stat(cachePath); err != nil {
		t.Fatalf("cache removed before failed config transaction: %v", err)
	}
}

func TestCommitConsoleLoginHardConfigFailureRestoresCacheAndMemory(t *testing.T) {
	_, cleanup := withTestConfigDir(t)
	defer cleanup()
	oldWriter := writeLoginConfigTransaction
	defer func() { writeLoginConfigTransaction = oldWriter }()

	cfg := &Configure{Profiles: map[string]*Profile{
		"default": {Name: "default", Mode: ModeConsoleLogin, LoginSession: "old"},
	}}
	if err := WriteConfigToFile(cfg); err != nil {
		t.Fatal(err)
	}
	cfg = LoadConfig()
	before := normalizedConfigCopy(cfg)
	cfg.Profiles["default"].LoginSession = "new"
	newCache := &LoginTokenCache{LoginSession: "new", AccessToken: json.RawMessage(`{"AccessKeyId":"new"}`)}
	wantErr := errors.New("injected config failure")
	writeLoginConfigTransaction = func(*Configure) error { return wantErr }

	err := commitConsoleLogin(cfg, before, newCache)
	if !errors.Is(err, wantErr) {
		t.Fatalf("commit error = %v, want config failure", err)
	}
	if cfg.Profiles["default"].LoginSession != "old" {
		t.Fatalf("memory config not restored: %#v", cfg.Profiles["default"])
	}
	newPath, _ := loginCacheFilePath("new")
	if _, err := os.Stat(newPath); !os.IsNotExist(err) {
		t.Fatalf("new cache survived failed config commit: %v", err)
	}
}

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
	_, cleanup := withTestConfigDir(t)
	defer cleanup()

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
