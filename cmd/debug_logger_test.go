package cmd

import (
	"bytes"
	"errors"
	"io/ioutil"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"unicode/utf8"
)

func TestResolveDebugOptionsFlagOverridesEnv(t *testing.T) {
	defer setenvForTest(t, envCLIDebug, "true")()
	defer setenvForTest(t, envCLIDebugLogFile, "env-debug.log")()

	ctx := NewContext()
	debugFlag, _ := ctx.fixedFlags.AddByName("debug")
	debugFlag.SetValue("false")
	logFileFlag, _ := ctx.fixedFlags.AddByName("debug-log-file")
	logFileFlag.SetValue("flag-debug.log")

	opts, err := resolveDebugOptions(ctx)
	if err != nil {
		t.Fatalf("resolveDebugOptions returned error: %v", err)
	}
	if opts.Enabled {
		t.Fatal("expected ---debug false to override enabled env debug")
	}
	if opts.LogFile != "flag-debug.log" {
		t.Fatalf("expected flag log file, got %q", opts.LogFile)
	}
}

func TestResolveDebugOptionsAcceptsTruthyEnvValues(t *testing.T) {
	defer setenvForTest(t, envCLIDebug, "TRUE")()

	opts, err := resolveDebugOptions(NewContext())
	if err != nil {
		t.Fatalf("resolveDebugOptions returned error: %v", err)
	}
	if !opts.Enabled {
		t.Fatal("expected TRUE env value to enable debug")
	}
}

func TestResolveDebugOptionsRejectsInvalidBool(t *testing.T) {
	defer setenvForTest(t, envCLIDebug, "maybe")()

	_, err := resolveDebugOptions(NewContext())
	if err == nil {
		t.Fatal("expected invalid debug env value to return error")
	}
	if !strings.Contains(err.Error(), envCLIDebug) {
		t.Fatalf("expected error to mention env name, got %v", err)
	}
}

func TestDebugLoggerDefaultsToStderrWriter(t *testing.T) {
	var stderr bytes.Buffer
	logger, err := newDebugLogger(debugOptions{Enabled: true}, &stderr)
	if err != nil {
		t.Fatalf("newDebugLogger returned error: %v", err)
	}
	defer logger.Close()

	logger.Printf("region=%s", "cn-beijing")
	if got := stderr.String(); !strings.Contains(got, "region=cn-beijing") {
		t.Fatalf("expected debug output in stderr writer, got %q", got)
	}
}

func TestDebugLoggerWritesExplicitFile(t *testing.T) {
	dir := tempDirForTest(t)
	defer cleanupDirForTest(dir)()
	logPath := filepath.Join(dir, "ve-debug.log")

	logger, err := newDebugLogger(debugOptions{Enabled: true, LogFile: logPath}, &bytes.Buffer{})
	if err != nil {
		t.Fatalf("newDebugLogger returned error: %v", err)
	}
	logger.Printf("debug line")
	if err := logger.Close(); err != nil {
		t.Fatalf("close debug logger: %v", err)
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

	_, err := newDebugLogger(debugOptions{Enabled: true, LogFile: linkPath}, &bytes.Buffer{})
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

	_, err := newDebugLogger(debugOptions{Enabled: true, LogFile: linkPath}, &bytes.Buffer{})
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
	dir := tempDirForTest(t)
	defer cleanupDirForTest(dir)()
	logPath := filepath.Join(dir, "ve-debug.log")

	logger, err := newDebugLogger(debugOptions{Enabled: false, LogFile: logPath}, &bytes.Buffer{})
	if err != nil {
		t.Fatalf("newDebugLogger returned error: %v", err)
	}
	logger.Printf("should not be written")
	if err := logger.Close(); err != nil {
		t.Fatalf("close debug logger: %v", err)
	}

	if _, err := os.Stat(logPath); !os.IsNotExist(err) {
		t.Fatalf("expected disabled debug not to create log file, stat err=%v", err)
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
