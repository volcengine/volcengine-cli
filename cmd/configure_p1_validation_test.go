package cmd

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func isolateColorConfig(t *testing.T) string {
	t.Helper()
	dir, cleanup := withTestConfigDir(t)
	t.Cleanup(cleanup)
	oldCtx, oldConfig := ctx, config
	ctx, config = &Context{}, nil
	t.Cleanup(func() { ctx, config = oldCtx, oldConfig })
	return filepath.Join(dir, ConfigFile)
}

func requireConfigErrorChain(t *testing.T, err error) {
	t.Helper()
	var wrapper *configLoadError
	if !errors.As(err, &wrapper) || wrapper.Unwrap() == nil {
		t.Fatal("configuration error must preserve its cause")
	}
	if !errors.Is(err, wrapper.Unwrap()) {
		t.Fatal("configuration cause must be discoverable with errors.Is")
	}
	if strings.Contains(err.Error(), "REDACTED") {
		t.Fatal("configuration error exposed a fixture value")
	}
}

func TestSetEnableColorValidation(t *testing.T) {
	invalid := []struct{ name, text string }{
		{"truncated_json", `{"current":"<REDACTED>","profiles":`},
		{"boolean_type", `{"enableColor":"<REDACTED>"}`},
		{"current_type", `{"current":{"<REDACTED>":true}}`},
		{"profiles_type", `{"profiles":"<REDACTED>"}`},
		{"sso_sessions_type", `{"sso-session":"<REDACTED>"}`},
		{"profile_field_type", `{"profiles":{"p":{"access-key":{"<REDACTED>":true}}}}`},
	}
	for _, enabled := range []bool{true, false} {
		mode := "disable"
		if enabled {
			mode = "enable"
		}
		for _, item := range invalid {
			t.Run(mode+"/"+item.name, func(t *testing.T) {
				path := isolateColorConfig(t)
				original := []byte(" \n" + item.text + "\n")
				if err := os.WriteFile(path, original, 0600); err != nil {
					t.Fatal("cannot prepare invalid configuration")
				}
				err := setEnableColor(enabled)
				if err == nil {
					t.Fatal("invalid configuration must reject color update")
				}
				requireConfigErrorChain(t, err)
				if item.name == "truncated_json" {
					var cause *json.SyntaxError
					if !errors.As(err, &cause) {
						t.Fatal("syntax error cause was lost")
					}
				} else {
					var cause *json.UnmarshalTypeError
					if !errors.As(err, &cause) {
						t.Fatal("type error cause was lost")
					}
				}
				after, readErr := os.ReadFile(path)
				if readErr != nil || !bytes.Equal(original, after) {
					t.Fatal("rejected update changed original file bytes")
				}
				if runtimeConfig() != nil {
					t.Fatal("rejected update published runtime configuration")
				}
			})
		}
		t.Run(mode+"/first_use", func(t *testing.T) {
			path := isolateColorConfig(t)
			if err := setEnableColor(enabled); err != nil {
				t.Fatal("first color update failed")
			}
			data, err := os.ReadFile(path)
			var saved Configure
			if err != nil || json.Unmarshal(data, &saved) != nil {
				t.Fatal("first update did not persist valid JSON")
			}
			if saved.EnableColor != enabled || saved.Profiles == nil || len(saved.Profiles) != 0 || saved.Current != "" {
				t.Fatal("unexpected first-use configuration")
			}
			if ctx.config != config || config == nil || !reflect.DeepEqual(*config, saved) {
				t.Fatal("runtime configuration does not match persisted configuration")
			}
		})
		for _, loaded := range []bool{false, true} {
			state := "reload"
			if loaded {
				state = "loaded"
			}
			t.Run(mode+"/preserve_sso_"+state, func(t *testing.T) {
				requireSSOSessionPreserved(t, enabled, loaded)
			})
			t.Run(mode+"/preserve_"+state, func(t *testing.T) {
				path := isolateColorConfig(t)
				disableSSL := false
				want := Configure{Current: "primary", EnableColor: !enabled, Profiles: map[string]*Profile{
					"primary":   {Name: "primary", Mode: ModeAK, AccessKey: "<REDACTED_AK>", SecretKey: "<REDACTED_SK>", Region: "test-region", Endpoint: "https://example.invalid", DisableSSL: &disableSSL},
					"secondary": {Name: "secondary", Mode: ModeAK, Region: "other-region"},
				}}
				data, err := json.Marshal(want)
				if err != nil || os.WriteFile(path, data, 0600) != nil {
					t.Fatal("cannot prepare valid configuration")
				}
				if loaded {
					cfg := LoadConfig()
					if cfg == nil {
						t.Fatal("valid configuration failed to load")
					}
					setRuntimeConfig(cfg)
				}
				if err := setEnableColor(enabled); err != nil {
					t.Fatal("valid configuration color update failed")
				}
				want.EnableColor = enabled
				saved := LoadConfig()
				if saved == nil || !reflect.DeepEqual(*saved, want) || !reflect.DeepEqual(runtimeConfig(), saved) {
					t.Fatal("color update lost profile, current, or runtime state")
				}
			})
		}
	}
}

func TestLoadConfigValidation(t *testing.T) {
	for _, public := range []bool{false, true} {
		name := "internal"
		if public {
			name = "public"
		}
		t.Run(name+"/missing", func(t *testing.T) {
			path := isolateColorConfig(t)
			var cfg *Configure
			var err error
			if public {
				cfg = LoadConfig()
			} else {
				cfg, err = loadConfig()
			}
			if cfg != nil || err != nil {
				t.Fatal("missing configuration must return nil without error")
			}
			if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
				t.Fatal("reading missing configuration must not create the file")
			}
		})
	}
	for _, stage := range []string{"locate", "prepare", "open", "read"} {
		t.Run(stage, func(t *testing.T) {
			path := isolateColorConfig(t)
			cause := &os.PathError{Op: "fixture", Path: "<REDACTED>", Err: os.ErrInvalid}
			switch stage {
			case "locate":
				configFileDirFunc = func() (string, error) { return "", cause }
			case "prepare":
				if os.WriteFile(path, nil, 0600) != nil {
					t.Fatal("cannot prepare directory failure")
				}
				configFileDirFunc = func() (string, error) { return path, nil }
			case "open":
				if os.Symlink(ConfigFile, path) != nil {
					t.Fatal("cannot prepare self-referencing fixture")
				}
			case "read":
				if os.Mkdir(path, 0700) != nil {
					t.Fatal("cannot prepare unreadable configuration")
				}
			}
			cfg, err := loadConfig()
			if cfg != nil || err == nil {
				t.Fatal("failed load must return error and no configuration")
			}
			requireConfigErrorChain(t, err)
			var pathErr *os.PathError
			if !errors.As(err, &pathErr) {
				t.Fatal("filesystem error type was lost")
			}
			if stage == "locate" && (!errors.Is(err, cause) || !errors.Is(err, os.ErrInvalid)) {
				t.Fatal("nested filesystem cause was lost")
			}
			if LoadConfig() != nil {
				t.Fatal("compatibility entry must not return invalid configuration")
			}
		})
	}
}

func requireSSOSessionPreserved(t *testing.T, enabled, loaded bool) {
	t.Helper()
	path := isolateColorConfig(t)
	disableSSL, dualStack := false, true
	want := Configure{
		Current:     "sso-primary",
		EnableColor: !enabled,
		Profiles: map[string]*Profile{
			"sso-primary": {
				Name: "sso-primary", Mode: ModeSSO, SsoSessionName: "session-primary",
				AccountId: "<REDACTED_ACCOUNT>", RoleName: "fixture-role", Region: "test-region",
				AccessKey: "<REDACTED_AK>", SecretKey: "<REDACTED_SK>",
				SessionToken: "<REDACTED_SESSION>", StsExpiration: 2000000000,
				DisableSSL: &disableSSL, UseDualStack: &dualStack,
			},
			"ak-secondary": {Name: "ak-secondary", Mode: ModeAK, AccessKey: "<REDACTED_OTHER_AK>", SecretKey: "<REDACTED_OTHER_SK>"},
		},
		SsoSession: map[string]*SsoSession{
			"session-primary":   {Name: "session-primary", StartURL: "https://example.invalid/sso/start", Region: "test-region", RegistrationScopes: []string{"sso:account:access", "fixture:secondary"}},
			"session-secondary": {Name: "session-secondary", StartURL: "https://example.invalid/sso/other", Region: "other-region", RegistrationScopes: []string{"sso:account:access"}},
		},
	}
	data, err := json.Marshal(want)
	if err != nil || os.WriteFile(path, data, 0600) != nil {
		t.Fatal("cannot prepare SSO configuration")
	}
	if loaded {
		loadedConfig, loadErr := loadConfig()
		if loadErr != nil || loadedConfig == nil {
			t.Fatal("cannot preload SSO configuration")
		}
		setRuntimeConfig(loadedConfig)
	} else if runtimeConfig() != nil {
		t.Fatal("SSO reload fixture must begin without runtime configuration")
	}
	if err := setEnableColor(enabled); err != nil {
		t.Fatal("SSO color update failed")
	}
	savedData, err := os.ReadFile(path)
	var saved Configure
	if err != nil || json.Unmarshal(savedData, &saved) != nil {
		t.Fatal("SSO color update did not persist valid configuration")
	}
	want.EnableColor = enabled
	if len(saved.SsoSession) != 2 || !reflect.DeepEqual(saved.SsoSession, want.SsoSession) {
		t.Fatal("color update changed persisted SSO sessions or registration scopes")
	}
	if saved.Current != "sso-primary" || !reflect.DeepEqual(saved.Profiles, want.Profiles) {
		t.Fatal("color update changed SSO profile linkage, temporary credentials, or other profiles")
	}
	if !reflect.DeepEqual(saved, want) {
		t.Fatal("persisted SSO configuration differs from expected state")
	}
	runtime := runtimeConfig()
	if runtime == nil || ctx.config != config || !reflect.DeepEqual(runtime, &want) {
		t.Fatal("runtime SSO configuration is not synchronized with persisted state")
	}
	if len(runtime.SsoSession) != 2 || !reflect.DeepEqual(runtime.SsoSession, want.SsoSession) {
		t.Fatal("runtime SSO sessions were not preserved")
	}
}
