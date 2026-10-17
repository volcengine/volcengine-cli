package cmd

import (
	"encoding/json"
	"io/ioutil"
	"os"
	"path/filepath"
	"testing"
)

// Loading config on a fresh install must not leave an empty config.json behind.
func TestLoadConfigDoesNotCreateMissingFile(t *testing.T) {
	dir, cleanup := withTestConfigDir(t)
	defer cleanup()

	if cfg := LoadConfig(); cfg != nil {
		t.Fatalf("LoadConfig() = %#v, want nil when config.json is missing", cfg)
	}
	if _, err := os.Stat(filepath.Join(dir, ConfigFile)); !os.IsNotExist(err) {
		t.Fatalf("config.json should not be created by LoadConfig, stat err = %v", err)
	}
}

// The color commands must work without an existing config and write valid JSON.
func TestSetEnableColorInitializesMissingConfig(t *testing.T) {
	dir, cleanup := withTestConfigDir(t)
	defer cleanup()
	oldCtxConfig, oldConfig := ctx.config, config
	ctx.config, config = nil, nil
	defer func() { ctx.config, config = oldCtxConfig, oldConfig }()

	if err := setEnableColor(true); err != nil {
		t.Fatalf("setEnableColor(true): %v", err)
	}
	data, err := ioutil.ReadFile(filepath.Join(dir, ConfigFile))
	if err != nil {
		t.Fatalf("read config.json: %v", err)
	}
	var saved Configure
	if err := json.Unmarshal(data, &saved); err != nil {
		t.Fatalf("config.json is not valid JSON: %v\n%s", err, data)
	}
	if !saved.EnableColor {
		t.Fatalf("enableColor = false, want true\n%s", data)
	}
	if runtimeConfig() == nil || !runtimeConfig().EnableColor {
		t.Fatal("runtime config should reflect the saved color preference")
	}
}
