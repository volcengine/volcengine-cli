package cmd

import (
	"bytes"
	"encoding/json"
	"errors"
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Older releases created an empty config.json on every start. Such a file
// means "not configured yet" and must not break loading or the first write.
func TestEmptyConfigFileIsTreatedAsMissing(t *testing.T) {
	for _, content := range []string{"", " \n\t\n"} {
		path := isolateColorConfig(t)
		if err := os.WriteFile(path, []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
		if cfg, err := loadConfig(); cfg != nil || err != nil {
			t.Fatalf("loadConfig() with %q = %#v, %v; want nil, nil", content, cfg, err)
		}
		if err := setEnableColor(true); err != nil {
			t.Fatalf("setEnableColor with %q config: %v", content, err)
		}
		var saved Configure
		data, err := ioutil.ReadFile(path)
		if err != nil || json.Unmarshal(data, &saved) != nil || !saved.EnableColor {
			t.Fatalf("empty config was not replaced with valid JSON: %q", data)
		}
	}
}

// A config file that exists but cannot be parsed was never loaded, so every
// writer must refuse to replace it instead of dropping its profiles.
func TestWritersDoNotOverwriteUnparseableConfig(t *testing.T) {
	original := []byte(`{"current":"p1","profiles":{"p1":{"name":"p1","access-key":"<REDACTED>"`)
	writers := map[string]func() error{
		"WriteConfigToFile": func() error {
			return WriteConfigToFile(&Configure{Profiles: map[string]*Profile{}})
		},
		"setConfigProfile": func() error {
			return setConfigProfile(&Profile{Name: "p2", AccessKey: "AK", SecretKey: "SK", Region: "r"})
		},
		"setEnableColor": func() error { return setEnableColor(true) },
	}
	for name, write := range writers {
		t.Run(name, func(t *testing.T) {
			path := isolateColorConfig(t)
			if err := os.WriteFile(path, original, 0600); err != nil {
				t.Fatal(err)
			}
			err := write()
			if err == nil {
				t.Fatal("write over an unparseable config must fail")
			}
			var loadErr *configLoadError
			if !errors.As(err, &loadErr) || !strings.Contains(err.Error(), path) {
				t.Fatalf("error %q must wrap configLoadError and name %s", err, path)
			}
			if strings.Contains(err.Error(), "REDACTED") {
				t.Fatal("error exposed configuration content")
			}
			after, readErr := os.ReadFile(path)
			if readErr != nil || !bytes.Equal(after, original) {
				t.Fatal("unparseable config file was modified")
			}
			if matches, _ := filepath.Glob(filepath.Join(filepath.Dir(path), ".tmp-config-*")); len(matches) != 0 {
				t.Fatalf("temporary files left behind: %v", matches)
			}
		})
	}
}

// A valid existing config is still replaced normally.
func TestWriteConfigReplacesValidConfig(t *testing.T) {
	path := isolateColorConfig(t)
	if err := os.WriteFile(path, []byte(`{"current":"old","profiles":{}}`), 0600); err != nil {
		t.Fatal(err)
	}
	if err := WriteConfigToFile(&Configure{Current: "new", Profiles: map[string]*Profile{}}); err != nil {
		t.Fatalf("WriteConfigToFile: %v", err)
	}
	cfg, err := loadConfig()
	if err != nil || cfg == nil || cfg.Current != "new" {
		t.Fatalf("loadConfig() = %#v, %v; want current=new", cfg, err)
	}
}

// Read-only commands load the config on startup and must not create the
// config directory as a side effect.
func TestLoadConfigDoesNotCreateConfigDirectory(t *testing.T) {
	isolateColorConfig(t)
	dir := filepath.Join(t.TempDir(), "missing")
	old := configFileDirFunc
	configFileDirFunc = func() (string, error) { return dir, nil }
	t.Cleanup(func() { configFileDirFunc = old })

	if cfg, err := loadConfig(); cfg != nil || err != nil {
		t.Fatalf("loadConfig() = %#v, %v; want nil, nil", cfg, err)
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatalf("loadConfig created the config directory, stat err = %v", err)
	}
	if err := WriteConfigToFile(&Configure{Profiles: map[string]*Profile{}}); err != nil {
		t.Fatalf("WriteConfigToFile must create the directory: %v", err)
	}
	if info, err := os.Stat(dir); err != nil || !info.IsDir() {
		t.Fatalf("config directory not created by writer: %v", err)
	}
}

// Load errors name the file but never include its content.
func TestConfigLoadErrorNamesFileWithoutContent(t *testing.T) {
	path := isolateColorConfig(t)
	if err := os.WriteFile(path, []byte(`{"current":"<REDACTED>"`), 0600); err != nil {
		t.Fatal(err)
	}
	_, err := loadConfig()
	if err == nil || !strings.Contains(err.Error(), path) || strings.Contains(err.Error(), "REDACTED") {
		t.Fatalf("loadConfig error = %v; want file path without content", err)
	}
}

// The startup warning goes to stderr and names the file.
func TestWarnConfigLoadErrorWritesToStderr(t *testing.T) {
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	oldStderr := os.Stderr
	os.Stderr = w
	warnConfigLoadError(nil)
	warnConfigLoadError(&configLoadError{operation: "parse", path: "/tmp/x/config.json", err: errors.New("bad")})
	os.Stderr = oldStderr
	w.Close()
	out, _ := ioutil.ReadAll(r)
	if got := string(out); strings.Count(got, "warning:") != 1 || !strings.Contains(got, "/tmp/x/config.json") {
		t.Fatalf("stderr = %q; want exactly one warning naming the file", got)
	}
}
