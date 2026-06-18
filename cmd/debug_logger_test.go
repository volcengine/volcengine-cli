package cmd

import (
	"errors"
	"io/ioutil"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"unicode/utf8"
)

func TestResolveDebugOptionsUsesDebugEnvOnly(t *testing.T) {
	tests := []struct {
		name    string
		value   string
		enabled bool
	}{
		{name: "true", value: "TRUE", enabled: true},
		{name: "one", value: "1", enabled: true},
		{name: "custom", value: "enabled", enabled: true},
		{name: "false", value: "false", enabled: false},
		{name: "zero", value: "0", enabled: false},
		{name: "off", value: "off", enabled: false},
		{name: "no", value: "no", enabled: false},
		{name: "empty", value: "", enabled: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			defer setenvForTest(t, envCLIDebug, tt.value)()

			opts, err := resolveDebugOptions(NewContext())
			if err != nil {
				t.Fatalf("resolveDebugOptions returned error: %v", err)
			}
			if opts.Enabled != tt.enabled {
				t.Fatalf("Enabled = %v, want %v", opts.Enabled, tt.enabled)
			}
		})
	}
}

func TestResolveDebugOptionsDisabledWhenEnvUnset(t *testing.T) {
	defer unsetenvForTest(t, envCLIDebug)()

	opts, err := resolveDebugOptions(NewContext())
	if err != nil {
		t.Fatalf("resolveDebugOptions returned error: %v", err)
	}
	if opts.Enabled {
		t.Fatal("expected debug to be disabled when env is unset")
	}
}

func TestDebugLoggerWritesDefaultHourlyFile(t *testing.T) {
	configDir := tempDirForTest(t)
	defer cleanupDirForTest(configDir)()
	defer withConfigDirForTest(configDir)()

	logger, err := newDebugLogger(debugOptions{Enabled: true})
	if err != nil {
		t.Fatalf("newDebugLogger returned error: %v", err)
	}

	logger.Printf("debug line")
	if err := logger.Close(); err != nil {
		t.Fatalf("close debug logger: %v", err)
	}

	logsDir := filepath.Join(configDir, "logs")
	entries, err := ioutil.ReadDir(logsDir)
	if err != nil {
		t.Fatalf("read logs dir: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected one debug log file, got %d", len(entries))
	}
	logPath := filepath.Join(logsDir, entries[0].Name())
	if len(entries[0].Name()) != len("2006010215.log") || !strings.HasSuffix(entries[0].Name(), ".log") {
		t.Fatalf("unexpected debug log file name %q", entries[0].Name())
	}

	data, err := ioutil.ReadFile(logPath)
	if err != nil {
		t.Fatalf("read log file: %v", err)
	}
	if !strings.Contains(string(data), "debug line") {
		t.Fatalf("expected log file to contain debug line, got %q", string(data))
	}
	info, err := os.Stat(logPath)
	if err != nil {
		t.Fatalf("stat log file: %v", err)
	}
	if runtime.GOOS != "windows" && info.Mode().Perm() != 0600 {
		t.Fatalf("expected log file perm 0600, got %v", info.Mode().Perm())
	}
	dirInfo, err := os.Stat(logsDir)
	if err != nil {
		t.Fatalf("stat logs dir: %v", err)
	}
	if runtime.GOOS != "windows" && dirInfo.Mode().Perm() != 0700 {
		t.Fatalf("expected logs dir perm 0700, got %v", dirInfo.Mode().Perm())
	}
}

func TestDebugLoggerRejectsSymlinkLogFile(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink creation requires extra privileges on Windows")
	}

	dir := tempDirForTest(t)
	defer cleanupDirForTest(dir)()
	targetPath := filepath.Join(dir, "target.log")
	linkPath := filepath.Join(dir, "link.log")

	if err := ioutil.WriteFile(targetPath, []byte("existing"), 0600); err != nil {
		t.Fatalf("write target log file: %v", err)
	}
	if err := os.Symlink(targetPath, linkPath); err != nil {
		t.Skipf("create symlink: %v", err)
	}

	_, err := openDebugLogFile(linkPath)
	if err == nil {
		t.Fatal("expected symlink log path to be rejected")
	}
	if !strings.Contains(err.Error(), "symbolic link") {
		t.Fatalf("expected symlink error, got %v", err)
	}
}

func TestDebugLoggerRejectsHardLinkedLogFile(t *testing.T) {
	dir := tempDirForTest(t)
	defer cleanupDirForTest(dir)()
	targetPath := filepath.Join(dir, "target.log")
	linkPath := filepath.Join(dir, "link.log")

	if err := ioutil.WriteFile(targetPath, []byte("existing"), 0600); err != nil {
		t.Fatalf("write target log file: %v", err)
	}
	if err := os.Link(targetPath, linkPath); err != nil {
		t.Skipf("create hard link: %v", err)
	}

	_, err := openDebugLogFile(linkPath)
	if err == nil {
		t.Fatal("expected hard-linked log path to be rejected")
	}
	if !strings.Contains(err.Error(), "multiple hard links") {
		t.Fatalf("expected hard-link error, got %v", err)
	}
}

func TestDebugLoggerCloseRunsCloseAfterFlushError(t *testing.T) {
	closeCalled := false
	logger := &DebugLogger{
		enabled: true,
		flush: func() error {
			return errors.New("flush failed")
		},
		close: func() error {
			closeCalled = true
			return nil
		},
	}

	err := logger.Close()
	if err == nil || !strings.Contains(err.Error(), "flush failed") {
		t.Fatalf("expected flush error, got %v", err)
	}
	if !closeCalled {
		t.Fatal("expected Close to run close callback even after flush error")
	}
}

func TestDebugLoggerDisabledDoesNotCreateFile(t *testing.T) {
	configDir := tempDirForTest(t)
	defer cleanupDirForTest(configDir)()
	defer withConfigDirForTest(configDir)()

	logger, err := newDebugLogger(debugOptions{Enabled: false})
	if err != nil {
		t.Fatalf("newDebugLogger returned error: %v", err)
	}
	logger.Printf("should not be written")
	if err := logger.Close(); err != nil {
		t.Fatalf("close debug logger: %v", err)
	}

	logsDir := filepath.Join(configDir, "logs")
	if _, err := os.Stat(logsDir); !os.IsNotExist(err) {
		t.Fatalf("expected disabled debug not to create logs dir, stat err=%v", err)
	}
}

func TestDebugSanitizeMasksSensitiveFields(t *testing.T) {
	input := map[string]interface{}{
		"AccessKey":    "ak-value",
		"SessionToken": "token-value",
		"InstanceId":   "i-123",
	}

	got := formatDebugValue(input, 1024)
	if strings.Contains(got, "ak-value") || strings.Contains(got, "token-value") {
		t.Fatalf("expected sensitive values to be masked, got %s", got)
	}
	if !strings.Contains(got, "i-123") {
		t.Fatalf("expected non-sensitive value to remain, got %s", got)
	}
}

func TestDebugSanitizeMasksCommonSensitiveFieldNames(t *testing.T) {
	input := map[string]interface{}{
		"AK":         "ak-value",
		"SK":         "sk-value",
		"ApiKey":     "api-key-value",
		"PrivateKey": "private-key-value",
		"Pwd":        "pwd-value",
		"Passwd":     "passwd-value",
		"Signature":  "signature-value",
		"Bearer":     "bearer-value",
		"TaskId":     "task-123",
	}

	got := formatDebugValue(input, 2048)
	for _, secret := range []string{
		"ak-value",
		"sk-value",
		"api-key-value",
		"private-key-value",
		"pwd-value",
		"passwd-value",
		"signature-value",
		"bearer-value",
	} {
		if strings.Contains(got, secret) {
			t.Fatalf("expected sensitive value %q to be masked, got %s", secret, got)
		}
	}
	if !strings.Contains(got, "task-123") {
		t.Fatalf("expected non-sensitive TaskId value to remain, got %s", got)
	}
}

func TestDebugSanitizeMasksTypedNestedValues(t *testing.T) {
	input := map[string]interface{}{
		"Tags": []map[string]interface{}{
			{
				"password": "nested-password",
				"Name":     "safe-name",
			},
		},
	}

	got := formatDebugValue(input, 2048)
	if strings.Contains(got, "nested-password") {
		t.Fatalf("expected nested typed sensitive value to be masked, got %s", got)
	}
	if !strings.Contains(got, "safe-name") {
		t.Fatalf("expected non-sensitive nested value to remain, got %s", got)
	}
}

func TestDebugValueTruncatesLongContent(t *testing.T) {
	got := formatDebugValue(strings.Repeat("a", 20), 8)
	if !strings.Contains(got, "truncated") {
		t.Fatalf("expected truncated marker, got %q", got)
	}
	if strings.HasPrefix(got, strings.Repeat("a", 9)) {
		t.Fatalf("expected content to be truncated, got %q", got)
	}
}

func TestDebugValueTruncatesOnUTF8Boundary(t *testing.T) {
	got := formatDebugValue("火山引擎-debug", 5)
	if !strings.Contains(got, "truncated") {
		t.Fatalf("expected truncated marker, got %q", got)
	}
	if !utf8.ValidString(got) {
		t.Fatalf("expected truncated debug string to remain valid UTF-8, got %q", got)
	}
}
