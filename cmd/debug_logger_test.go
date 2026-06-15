package cmd

import (
	"bytes"
	"io/ioutil"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
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

func TestDebugValueTruncatesLongContent(t *testing.T) {
	got := formatDebugValue(strings.Repeat("a", 20), 8)
	if !strings.Contains(got, "truncated") {
		t.Fatalf("expected truncated marker, got %q", got)
	}
	if strings.HasPrefix(got, strings.Repeat("a", 9)) {
		t.Fatalf("expected content to be truncated, got %q", got)
	}
}
