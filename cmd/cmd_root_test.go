package cmd

import (
	"bytes"
	"os"
	"path/filepath"
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

func TestColorCommandsInitializeEmptyConfig(t *testing.T) {
	configDir := t.TempDir()
	previousConfigDirFunc := configFileDirFunc
	previousConfig := config
	previousContextConfig := ctx.config
	configFileDirFunc = func() (string, error) { return configDir, nil }
	config = nil
	ctx.config = nil
	t.Cleanup(func() {
		configFileDirFunc = previousConfigDirFunc
		config = previousConfig
		ctx.config = previousContextConfig
	})

	enable := newColorCommand("enable-color", true)
	if err := enable.RunE(enable, nil); err != nil {
		t.Fatalf("enable-color: %v", err)
	}
	if config == nil || !config.EnableColor || ctx.config != config {
		t.Fatalf("runtime config not initialized after enable: config=%#v ctx=%#v", config, ctx.config)
	}
	if config.Profiles == nil || config.SsoSession == nil {
		t.Fatalf("config maps not initialized: %#v", config)
	}

	loaded := LoadConfig()
	if loaded == nil || !loaded.EnableColor {
		t.Fatalf("persisted config = %#v, want enableColor=true at %s", loaded, filepath.Join(configDir, ConfigFile))
	}

	disable := newColorCommand("disable-color", false)
	if err := disable.RunE(disable, nil); err != nil {
		t.Fatalf("disable-color: %v", err)
	}
	loaded = LoadConfig()
	if loaded == nil || loaded.EnableColor {
		t.Fatalf("persisted config = %#v, want enableColor=false", loaded)
	}
}

func TestColorCommandsDoNotOverwriteCorruptConfig(t *testing.T) {
	configDir := t.TempDir()
	previousConfigDirFunc := configFileDirFunc
	previousConfig := config
	previousContextConfig := ctx.config
	configFileDirFunc = func() (string, error) { return configDir, nil }
	config = nil
	ctx.config = nil
	t.Cleanup(func() {
		configFileDirFunc = previousConfigDirFunc
		config = previousConfig
		ctx.config = previousContextConfig
	})

	corrupt := []byte(`{"current":"default","profiles":{"default":{"secret-key":"REALSK"}`)
	if err := os.WriteFile(filepath.Join(configDir, ConfigFile), corrupt, 0600); err != nil {
		t.Fatal(err)
	}
	enable := newColorCommand("enable-color", true)
	if err := enable.RunE(enable, nil); err == nil {
		t.Fatal("expected parse error for corrupt config")
	}
	got, err := os.ReadFile(filepath.Join(configDir, ConfigFile))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(corrupt) {
		t.Fatalf("corrupt config was rewritten: %s", got)
	}
}
