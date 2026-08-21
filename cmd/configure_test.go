package cmd

import (
	"bytes"
	"encoding/json"
	"errors"
	"io/ioutil"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/volcengine/volcengine-go-sdk/volcengine/credentials/clicreds"
)

// --------------- marshalConfig ---------------

func TestMarshalConfigUsesIndentedJSON(t *testing.T) {
	data, err := marshalConfig(&Configure{
		Current: "test",
		Profiles: map[string]*Profile{
			"test": {
				Name:      "test",
				Mode:      ModeAK,
				Region:    "cn-beijing",
				AccessKey: "ak",
				SecretKey: "sk",
			},
		},
	})
	if err != nil {
		t.Fatalf("marshalConfig returned error: %v", err)
	}

	if !json.Valid(data) {
		t.Fatalf("marshalConfig returned invalid json: %s", string(data))
	}
	if !strings.Contains(string(data), "\n") {
		t.Fatalf("marshalConfig should produce multi-line json, got: %s", string(data))
	}
	if !strings.Contains(string(data), "\n    \"profiles\":") {
		t.Fatalf("marshalConfig should indent top-level fields, got: %s", string(data))
	}
}

// --------------- validateProfileMode ---------------

func TestValidateProfileModeRequiresAkCredentialsByDefault(t *testing.T) {
	err := validateProfileMode(&Profile{
		Name:   "test",
		Region: "cn-beijing",
	})
	if err == nil {
		t.Fatal("expected default ak mode to require access-key and secret-key")
	}
}

func TestValidateProfileModeAllowsMissingRegion(t *testing.T) {
	err := validateProfileMode(&Profile{
		Name:      "test",
		Mode:      ModeAK,
		AccessKey: "ak",
		SecretKey: "sk",
	})
	if err != nil {
		t.Fatalf("expected missing region to be valid at configure set time, got: %v", err)
	}
}

func TestConfigureSetAllowsMissingRegionForNewAkProfile(t *testing.T) {
	_, cleanupConfigDir := withTestConfigDir(t)
	defer cleanupConfigDir()
	defer resetProfileFlagsForTest(t)()
	defer withTestCtxConfig(t, &Configure{
		Profiles: map[string]*Profile{},
	})()

	setCmd := newConfigureSetCmd()
	setCmd.SetArgs([]string{
		"--profile", "test",
		"--mode", ModeAK,
		"--access-key", "ak",
		"--secret-key", "sk",
	})

	err := setCmd.Execute()
	if err != nil {
		t.Fatalf("expected configure set to allow profile without region, got: %v", err)
	}

	cfg := runtimeConfig()
	p := cfg.Profiles["test"]
	if p == nil {
		t.Fatal("expected profile to be saved")
	}
	if p.Region != "" {
		t.Fatalf("expected region to remain empty when omitted, got %q", p.Region)
	}
}

func TestValidateProfileModeAkRequiresSecretKey(t *testing.T) {
	err := validateProfileMode(&Profile{
		Name:      "test",
		Mode:      ModeAK,
		AccessKey: "ak",
	})
	if err == nil {
		t.Fatal("expected ak mode to require secret-key")
	}
}

func TestValidateProfileModeAkValid(t *testing.T) {
	err := validateProfileMode(&Profile{
		Name:      "test",
		Mode:      ModeAK,
		AccessKey: "ak",
		SecretKey: "sk",
		Region:    "cn-beijing",
	})
	if err != nil {
		t.Fatalf("expected ak mode to be valid, got error: %v", err)
	}
}

func TestValidateProfileModeRamRoleArnRequiresAllFields(t *testing.T) {
	// missing account-id
	err := validateProfileMode(&Profile{
		Name:      "test",
		Mode:      ModeRamRoleArn,
		AccessKey: "ak",
		SecretKey: "sk",
		RoleName:  "role",
	})
	if err == nil {
		t.Fatal("expected ramrolearn mode to require account-id")
	}

	// missing role-name
	err = validateProfileMode(&Profile{
		Name:      "test",
		Mode:      ModeRamRoleArn,
		AccessKey: "ak",
		SecretKey: "sk",
		AccountId: "123",
	})
	if err == nil {
		t.Fatal("expected ramrolearn mode to require role-name")
	}
}

func TestValidateProfileModeRamRoleArnValid(t *testing.T) {
	err := validateProfileMode(&Profile{
		Name:      "test",
		Mode:      ModeRamRoleArn,
		AccessKey: "ak",
		SecretKey: "sk",
		RoleName:  "role",
		AccountId: "123",
		Region:    "cn-beijing",
	})
	if err != nil {
		t.Fatalf("expected ramrolearn mode to be valid, got error: %v", err)
	}
}

func TestValidateProfileModeOidcRequiresFields(t *testing.T) {
	err := validateProfileMode(&Profile{
		Name: "test",
		Mode: ModeOIDC,
	})
	if err == nil {
		t.Fatal("expected oidc mode to require oidc-token-file and role-trn")
	}

	err = validateProfileMode(&Profile{
		Name:          "test",
		Mode:          ModeOIDC,
		OidcTokenFile: "/tmp/token",
	})
	if err == nil {
		t.Fatal("expected oidc mode to require role-trn")
	}
}

func TestValidateProfileModeOidcValid(t *testing.T) {
	err := validateProfileMode(&Profile{
		Name:          "test",
		Mode:          ModeOIDC,
		OidcTokenFile: "/tmp/token",
		RoleTrn:       "trn:iam::2100000000:role/TestRole",
		Region:        "cn-beijing",
	})
	if err != nil {
		t.Fatalf("expected oidc mode to be valid, got error: %v", err)
	}
}

func TestValidateProfileModeEcsRoleRequiresRoleName(t *testing.T) {
	err := validateProfileMode(&Profile{
		Name: "test",
		Mode: ModeEcsRole,
	})
	if err == nil {
		t.Fatal("expected ecsrole mode to require role-name")
	}
}

func TestValidateProfileModeEcsRoleValid(t *testing.T) {
	err := validateProfileMode(&Profile{
		Name:     "test",
		Mode:     ModeEcsRole,
		RoleName: "role",
		Region:   "cn-beijing",
	})
	if err != nil {
		t.Fatalf("expected ecsrole mode to be valid, got error: %v", err)
	}
}

func TestValidateProfileModeConsoleLoginRequiresLoginSession(t *testing.T) {
	err := validateProfileMode(&Profile{
		Name: "test",
		Mode: ModeConsoleLogin,
	})
	if err == nil {
		t.Fatal("expected console-login mode to require login-session")
	}
	if !strings.Contains(err.Error(), "login-session") {
		t.Fatalf("expected error to mention login-session, got: %v", err)
	}
}

func TestValidateProfileModeConsoleLoginValid(t *testing.T) {
	err := validateProfileMode(&Profile{
		Name:         "test",
		Mode:         ModeConsoleLogin,
		LoginSession: "trn:iam::123456789012:login/session/test",
		Region:       "cn-beijing",
	})
	if err != nil {
		t.Fatalf("expected console-login mode to be valid, got error: %v", err)
	}
}

func TestValidateProfileModeUnsupported(t *testing.T) {
	err := validateProfileMode(&Profile{
		Name: "test",
		Mode: "invalid",
	})
	if err == nil {
		t.Fatal("expected unsupported mode to return error")
	}
	if !strings.Contains(err.Error(), "unsupported mode") {
		t.Fatalf("expected error to mention unsupported mode, got: %v", err)
	}
}

func TestValidateProfileModeRequiresOidcFieldsFromProfile(t *testing.T) {
	defer setenvForTest(t, "VOLCENGINE_OIDC_TOKEN_FILE", "/tmp/token")()
	defer setenvForTest(t, "VOLCENGINE_OIDC_ROLE_TRN", "trn:iam::2100000000:role/TestRole")()

	err := validateProfileMode(&Profile{
		Name: "test",
		Mode: ModeOIDC,
	})
	if err == nil {
		t.Fatal("expected oidc mode to require profile fields even when env vars exist")
	}
}

// --------------- mergeProfile ---------------

func TestMergeProfileKeepsExistingModeWhenUpdating(t *testing.T) {
	disableSSL := false
	useDualStack := false
	merged := mergeProfile(&Profile{
		Name:         "test",
		Mode:         ModeAK,
		AccessKey:    "ak",
		SecretKey:    "sk",
		DisableSSL:   &disableSSL,
		UseDualStack: &useDualStack,
	}, &Profile{
		Name:   "test",
		Region: "cn-beijing",
	})

	if err := validateProfileMode(merged); err != nil {
		t.Fatalf("expected merged profile to stay valid, got error: %v", err)
	}
	if merged.Mode != ModeAK {
		t.Fatalf("expected merged profile to keep mode %q, got %q", ModeAK, merged.Mode)
	}
	if merged.Region != "cn-beijing" {
		t.Fatalf("expected region to be updated, got %q", merged.Region)
	}
}

func TestMergeProfileDefaultsNewProfileToAk(t *testing.T) {
	merged := mergeProfile(nil, &Profile{
		Name:      "test",
		AccessKey: "ak",
		SecretKey: "sk",
	})
	if merged.Mode != ModeAK {
		t.Fatalf("expected new profile to default to mode %q, got %q", ModeAK, merged.Mode)
	}
}

func TestMergeProfilePreservesNonAkModeOnUpdate(t *testing.T) {
	disableSSL := false
	merged := mergeProfile(&Profile{
		Name:       "ecs",
		Mode:       ModeEcsRole,
		RoleName:   "MyRole",
		Region:     "cn-beijing",
		DisableSSL: &disableSSL,
	}, &Profile{
		Name:   "ecs",
		Region: "ap-southeast-1",
	})

	if merged.Mode != ModeEcsRole {
		t.Fatalf("expected mode to stay %q, got %q", ModeEcsRole, merged.Mode)
	}
	if merged.Region != "ap-southeast-1" {
		t.Fatalf("expected region to be updated, got %q", merged.Region)
	}
	if merged.RoleName != "MyRole" {
		t.Fatalf("expected role-name to be preserved, got %q", merged.RoleName)
	}
}

func TestMergeProfileDoesNotDefaultModeForExistingProfileWithoutMode(t *testing.T) {
	disableSSL := false
	merged := mergeProfile(&Profile{
		Name:       "old",
		AccessKey:  "ak",
		SecretKey:  "sk",
		Region:     "cn-beijing",
		DisableSSL: &disableSSL,
	}, &Profile{
		Name:   "old",
		Region: "cn-shanghai",
	})

	if merged.Mode != "" {
		t.Fatalf("expected mode to stay empty for existing profile without mode, got %q", merged.Mode)
	}
}

func TestMergeProfileClonesPointerFields(t *testing.T) {
	disableSSL := false
	useDualStack := true
	base := &Profile{
		Name:         "test",
		Mode:         ModeAK,
		AccessKey:    "ak",
		SecretKey:    "sk",
		DisableSSL:   &disableSSL,
		UseDualStack: &useDualStack,
	}

	merged := mergeProfile(base, &Profile{Name: "test"})

	*merged.DisableSSL = true
	if *base.DisableSSL != false {
		t.Fatal("mergeProfile should deep-clone pointer fields")
	}
}

func TestMergeProfileNilInput(t *testing.T) {
	disableSSL := false
	base := &Profile{
		Name:       "test",
		Mode:       ModeAK,
		AccessKey:  "ak",
		SecretKey:  "sk",
		DisableSSL: &disableSSL,
	}
	merged := mergeProfile(base, nil)
	if merged.Name != "test" || merged.AccessKey != "ak" {
		t.Fatal("mergeProfile with nil input should return clone of base")
	}
}

func TestMergeProfileOverrideMode(t *testing.T) {
	disableSSL := false
	merged := mergeProfile(&Profile{
		Name:       "test",
		Mode:       ModeAK,
		AccessKey:  "ak",
		SecretKey:  "sk",
		DisableSSL: &disableSSL,
	}, &Profile{
		Name:     "test",
		Mode:     ModeEcsRole,
		RoleName: "role",
	})

	if merged.Mode != ModeEcsRole {
		t.Fatalf("expected mode to be overridden to %q, got %q", ModeEcsRole, merged.Mode)
	}
}

// --------------- FlagSet.GetByName ---------------

func TestFlagSetGetByName(t *testing.T) {
	fs := NewFlagSet()
	f, _ := fs.AddByName("profile")
	f.SetValue("prod")

	got := fs.GetByName("profile")
	if got == nil {
		t.Fatal("expected GetByName to find flag")
	}
	if got.GetValue() != "prod" {
		t.Fatalf("expected value %q, got %q", "prod", got.GetValue())
	}

	if fs.GetByName("nonexistent") != nil {
		t.Fatal("expected GetByName to return nil for missing flag")
	}
}

// --------------- NewSimpleClient ---------------

func TestNewSimpleClientProfileOverrideNotFound(t *testing.T) {
	ctx := NewContext()
	disableSSL := false
	ctx.config = &Configure{
		Current: "default",
		Profiles: map[string]*Profile{
			"default": {
				Name:       "default",
				Mode:       ModeAK,
				AccessKey:  "ak",
				SecretKey:  "sk",
				Region:     "cn-beijing",
				DisableSSL: &disableSSL,
			},
		},
	}

	f, _ := ctx.fixedFlags.AddByName("profile")
	f.SetValue("nonexistent")

	_, err := NewSimpleClient(ctx)
	if err == nil {
		t.Fatal("expected error when ---profile specifies non-existent profile")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Fatalf("expected error to mention 'not found', got: %v", err)
	}
}

func TestNewSimpleClientProfileOverrideValid(t *testing.T) {
	ctx := NewContext()
	disableSSL := false
	ctx.config = &Configure{
		Current: "default",
		Profiles: map[string]*Profile{
			"default": {
				Name:       "default",
				Mode:       ModeAK,
				AccessKey:  "ak-default",
				SecretKey:  "sk-default",
				Region:     "cn-beijing",
				DisableSSL: &disableSSL,
			},
			"prod": {
				Name:       "prod",
				Mode:       ModeAK,
				AccessKey:  "ak-prod",
				SecretKey:  "sk-prod",
				Region:     "cn-shanghai",
				DisableSSL: &disableSSL,
			},
		},
	}

	f, _ := ctx.fixedFlags.AddByName("profile")
	f.SetValue("prod")

	client, err := NewSimpleClient(ctx)
	if err != nil {
		t.Fatalf("expected success, got error: %v", err)
	}
	if client == nil {
		t.Fatal("expected non-nil client")
	}
}

func TestNewSimpleClientRegionOverride(t *testing.T) {
	ctx := NewContext()
	disableSSL := false
	ctx.config = &Configure{
		Current: "default",
		Profiles: map[string]*Profile{
			"default": {
				Name:       "default",
				Mode:       ModeAK,
				AccessKey:  "ak",
				SecretKey:  "sk",
				Region:     "cn-beijing",
				DisableSSL: &disableSSL,
			},
		},
	}

	f, _ := ctx.fixedFlags.AddByName("region")
	f.SetValue("cn-shanghai")

	client, err := NewSimpleClient(ctx)
	if err != nil {
		t.Fatalf("expected success, got error: %v", err)
	}
	if *client.Config.Region != "cn-shanghai" {
		t.Fatalf("expected region to be overridden to cn-shanghai, got %q", *client.Config.Region)
	}
}

func TestNewSimpleClientEndpointOverride(t *testing.T) {
	ctx := NewContext()
	disableSSL := false
	ctx.config = &Configure{
		Current: "default",
		Profiles: map[string]*Profile{
			"default": {
				Name:       "default",
				Mode:       ModeAK,
				AccessKey:  "ak",
				SecretKey:  "sk",
				Region:     "cn-beijing",
				Endpoint:   "profile.example.com",
				DisableSSL: &disableSSL,
			},
		},
	}

	f, _ := ctx.fixedFlags.AddByName("endpoint")
	f.SetValue("override.example.com")

	client, err := NewSimpleClient(ctx)
	if err != nil {
		t.Fatalf("expected success, got error: %v", err)
	}
	if *client.Config.Endpoint != "override.example.com" {
		t.Fatalf("expected endpoint override, got %q", *client.Config.Endpoint)
	}
}

func TestNewSimpleClientProfileProxyConfig(t *testing.T) {
	ctx := NewContext()
	disableSSL := false
	ctx.config = &Configure{
		Current: "default",
		Profiles: map[string]*Profile{
			"default": {
				Name:       "default",
				Mode:       ModeAK,
				AccessKey:  "ak",
				SecretKey:  "sk",
				Region:     "cn-beijing",
				HTTPProxy:  "http://127.0.0.1:8080",
				HTTPSProxy: "http://127.0.0.1:8443",
				DisableSSL: &disableSSL,
			},
		},
	}

	client, err := NewSimpleClient(ctx)
	if err != nil {
		t.Fatalf("expected success, got error: %v", err)
	}
	if client.Config.HTTPProxy == nil || *client.Config.HTTPProxy != "http://127.0.0.1:8080" {
		t.Fatalf("expected HTTP proxy to be set, got %v", client.Config.HTTPProxy)
	}
	if client.Config.HTTPSProxy == nil || *client.Config.HTTPSProxy != "http://127.0.0.1:8443" {
		t.Fatalf("expected HTTPS proxy to be set, got %v", client.Config.HTTPSProxy)
	}
}

func TestNewSimpleClientNoProfileAllowsDefaultChainWithoutExplicitCredentials(t *testing.T) {
	defer setenvForTest(t, "VOLCENGINE_ACCESS_KEY", "")()
	defer setenvForTest(t, "VOLCENGINE_SECRET_KEY", "")()
	defer setenvForTest(t, "VOLCENGINE_REGION", "cn-beijing")()
	defer setenvForTest(t, "VOLCENGINE_PROFILE", "")()
	defer setenvForTest(t, "VOLCENGINE_OIDC_TOKEN_FILE", "")()
	defer setenvForTest(t, "VOLCENGINE_OIDC_ROLE_TRN", "")()
	defer setenvForTest(t, "VOLCENGINE_ECS_METADATA", "")()
	defer setenvForTest(t, "VOLCSTACK_ACCESS_KEY_ID", "")()
	defer setenvForTest(t, "VOLCSTACK_ACCESS_KEY", "")()
	defer setenvForTest(t, "VOLCSTACK_PROFILE", "")()
	defer setenvForTest(t, "VOLCSTACK_CONTAINER_CREDENTIALS_FULL_URI", "")()

	ctx := NewContext()
	ctx.config = &Configure{
		Current:  "",
		Profiles: map[string]*Profile{},
	}

	// Default chain providers, including IMDS, resolve credentials when a request is sent.
	// Client construction must not require an explicit local credential signal.
	client, err := NewSimpleClient(ctx)
	if err != nil {
		t.Fatalf("expected default chain client construction to succeed, got: %v", err)
	}
	if client == nil {
		t.Fatal("expected non-nil client")
	}
}

func TestNewSimpleClientNoProfileMissingCredentialAndRegionReportsCredentialFirst(t *testing.T) {
	defer setenvForTest(t, "VOLCENGINE_ACCESS_KEY", "")()
	defer setenvForTest(t, "VOLCENGINE_SECRET_KEY", "")()
	defer setenvForTest(t, "VOLCENGINE_REGION", "")()
	defer setenvForTest(t, "VOLCENGINE_PROFILE", "")()
	defer setenvForTest(t, "VOLCENGINE_OIDC_TOKEN_FILE", "")()
	defer setenvForTest(t, "VOLCENGINE_OIDC_ROLE_TRN", "")()
	defer setenvForTest(t, "VOLCENGINE_ECS_METADATA", "")()
	defer setenvForTest(t, "VOLCSTACK_ACCESS_KEY_ID", "")()
	defer setenvForTest(t, "VOLCSTACK_ACCESS_KEY", "")()
	defer setenvForTest(t, "VOLCSTACK_PROFILE", "")()
	defer setenvForTest(t, "VOLCSTACK_CONTAINER_CREDENTIALS_FULL_URI", "")()

	ctx := NewContext()
	ctx.config = &Configure{
		Current:  "",
		Profiles: map[string]*Profile{},
	}

	_, err := NewSimpleClient(ctx)
	if err == nil {
		t.Fatal("expected credentials error")
	}
	if !strings.Contains(err.Error(), "credentials not configured") {
		t.Fatalf("expected credentials guidance before region guidance, got: %v", err)
	}
}

func TestNewSimpleClientEmptyCurrentIgnoresDefaultProfile(t *testing.T) {
	defer setenvForTest(t, "VOLCENGINE_ACCESS_KEY", "env-ak")()
	defer setenvForTest(t, "VOLCENGINE_SECRET_KEY", "env-sk")()
	defer setenvForTest(t, "VOLCENGINE_REGION", "cn-shanghai")()
	defer setenvForTest(t, "VOLCENGINE_PROFILE", "")()
	defer setenvForTest(t, "VOLCSTACK_PROFILE", "")()

	ctx := NewContext()
	ctx.config = &Configure{
		Current: "",
		Profiles: map[string]*Profile{
			"default": {
				Name:      "default",
				Mode:      ModeAK,
				AccessKey: "ak",
				SecretKey: "sk",
				Region:    "cn-beijing",
			},
		},
	}

	// Empty Current must fall back to the default credential chain, NOT silently
	// adopt the "default" profile. Region therefore comes from the environment
	// (cn-shanghai), not from the default profile (cn-beijing).
	client, err := NewSimpleClient(ctx)
	if err != nil {
		t.Fatalf("expected default credential chain, got: %v", err)
	}
	if *client.Config.Region != "cn-shanghai" {
		t.Fatalf("expected env region cn-shanghai (default chain), got %q", *client.Config.Region)
	}
}

func TestNewSimpleClientCurrentTakesPriorityOverEnvProfile(t *testing.T) {
	defer setenvForTest(t, "VOLCENGINE_PROFILE", "prod")()
	defer setenvForTest(t, "VOLCSTACK_PROFILE", "")()

	ctx := NewContext()
	ctx.config = &Configure{
		Current: "default",
		Profiles: map[string]*Profile{
			"default": {
				Name:      "default",
				Mode:      ModeAK,
				AccessKey: "ak-default",
				SecretKey: "sk-default",
				Region:    "cn-beijing",
			},
			"prod": {
				Name:      "prod",
				Mode:      ModeAK,
				AccessKey: "ak-prod",
				SecretKey: "sk-prod",
				Region:    "cn-shanghai",
			},
		},
	}

	// Profile selection priority is ---profile > Current > VOLCENGINE_PROFILE, so
	// the configured Current (default) wins over VOLCENGINE_PROFILE=prod.
	client, err := NewSimpleClient(ctx)
	if err != nil {
		t.Fatalf("expected current profile selection, got: %v", err)
	}
	if *client.Config.Region != "cn-beijing" {
		t.Fatalf("expected current (default) profile region cn-beijing, got %q", *client.Config.Region)
	}
}

func TestNewSimpleClientEnvProfileUsedWhenCurrentEmpty(t *testing.T) {
	defer setenvForTest(t, "VOLCENGINE_PROFILE", "prod")()
	defer setenvForTest(t, "VOLCSTACK_PROFILE", "")()

	ctx := NewContext()
	ctx.config = &Configure{
		Current: "",
		Profiles: map[string]*Profile{
			"prod": {
				Name:      "prod",
				Mode:      ModeAK,
				AccessKey: "ak-prod",
				SecretKey: "sk-prod",
				Region:    "cn-shanghai",
			},
		},
	}

	// With empty Current, VOLCENGINE_PROFILE selects the profile.
	client, err := NewSimpleClient(ctx)
	if err != nil {
		t.Fatalf("expected env profile selection, got: %v", err)
	}
	if *client.Config.Region != "cn-shanghai" {
		t.Fatalf("expected prod profile region cn-shanghai, got %q", *client.Config.Region)
	}
}

func TestNewSimpleClientProfileMissingEndpointFallsBackToEnv(t *testing.T) {
	defer setenvForTest(t, "VOLCENGINE_PROFILE", "")()
	defer setenvForTest(t, "VOLCSTACK_PROFILE", "")()
	defer setenvForTest(t, "VOLCENGINE_ENDPOINT", "env.example.com")()

	ctx := NewContext()
	disableSSL := false
	ctx.config = &Configure{
		Current: "default",
		Profiles: map[string]*Profile{
			"default": {
				Name:       "default",
				Mode:       ModeAK,
				AccessKey:  "ak",
				SecretKey:  "sk",
				Region:     "cn-beijing",
				DisableSSL: &disableSSL,
			},
		},
	}

	// Endpoint priority is ---endpoint > profile.Endpoint > VOLCENGINE_ENDPOINT, so
	// an empty profile endpoint falls back to the environment variable.
	client, err := NewSimpleClient(ctx)
	if err != nil {
		t.Fatalf("expected success, got error: %v", err)
	}
	if client.Config.Endpoint == nil || *client.Config.Endpoint != "env.example.com" {
		t.Fatalf("expected env endpoint env.example.com, got %v", client.Config.Endpoint)
	}
}

func TestNewSimpleClientNoProfileUsesDefaultChain(t *testing.T) {
	defer setenvForTest(t, "VOLCENGINE_ACCESS_KEY", "env-ak")()
	defer setenvForTest(t, "VOLCENGINE_SECRET_KEY", "env-sk")()
	defer setenvForTest(t, "VOLCENGINE_REGION", "cn-beijing")()

	ctx := NewContext()
	ctx.config = &Configure{
		Current:  "",
		Profiles: map[string]*Profile{},
	}

	client, err := NewSimpleClient(ctx)
	if err != nil {
		t.Fatalf("expected default chain to work, got error: %v", err)
	}
	if client == nil {
		t.Fatal("expected non-nil client")
	}
}

func TestNewSimpleClientNoProfileMissingRegion(t *testing.T) {
	defer setenvForTest(t, "VOLCENGINE_ACCESS_KEY", "env-ak")()
	defer setenvForTest(t, "VOLCENGINE_SECRET_KEY", "env-sk")()
	defer setenvForTest(t, "VOLCENGINE_REGION", "")()

	ctx := NewContext()
	ctx.config = &Configure{
		Current:  "",
		Profiles: map[string]*Profile{},
	}

	_, err := NewSimpleClient(ctx)
	if err == nil {
		t.Fatal("expected error when region is not set")
	}
	if !strings.Contains(err.Error(), "region not set") {
		t.Fatalf("expected error to mention region not set, got: %v", err)
	}
}

func TestNewSimpleClientProfileMissingRegion(t *testing.T) {
	defer setenvForTest(t, "VOLCENGINE_REGION", "")()

	ctx := NewContext()
	disableSSL := false
	ctx.config = &Configure{
		Current: "test",
		Profiles: map[string]*Profile{
			"test": {
				Name:       "test",
				Mode:       ModeAK,
				AccessKey:  "ak",
				SecretKey:  "sk",
				DisableSSL: &disableSSL,
			},
		},
	}

	_, err := NewSimpleClient(ctx)
	if err == nil {
		t.Fatal("expected error when profile region is not set")
	}
	if !strings.Contains(err.Error(), "region not set") {
		t.Fatalf("expected error to mention region, got: %v", err)
	}
}

func TestNewSimpleClientProfileMissingRegionFallsBackToEnv(t *testing.T) {
	defer setenvForTest(t, "VOLCENGINE_REGION", "cn-beijing")()

	ctx := NewContext()
	disableSSL := false
	ctx.config = &Configure{
		Current: "test",
		Profiles: map[string]*Profile{
			"test": {
				Name:       "test",
				Mode:       ModeAK,
				AccessKey:  "ak",
				SecretKey:  "sk",
				DisableSSL: &disableSSL,
			},
		},
	}

	client, err := NewSimpleClient(ctx)
	if err != nil {
		t.Fatalf("expected empty profile region to fall back to VOLCENGINE_REGION, got error: %v", err)
	}
	if *client.Config.Region != "cn-beijing" {
		t.Fatalf("expected region cn-beijing from env, got %q", *client.Config.Region)
	}
}

func TestNewSimpleClientRegionOverrideFixesEmptyProfileRegion(t *testing.T) {
	ctx := NewContext()
	disableSSL := false
	ctx.config = &Configure{
		Current: "test",
		Profiles: map[string]*Profile{
			"test": {
				Name:       "test",
				Mode:       ModeAK,
				AccessKey:  "ak",
				SecretKey:  "sk",
				DisableSSL: &disableSSL,
			},
		},
	}

	f, _ := ctx.fixedFlags.AddByName("region")
	f.SetValue("cn-shanghai")

	client, err := NewSimpleClient(ctx)
	if err != nil {
		t.Fatalf("expected ---region to override empty profile region, got error: %v", err)
	}
	if *client.Config.Region != "cn-shanghai" {
		t.Fatalf("expected region cn-shanghai, got %q", *client.Config.Region)
	}
}

func TestNewSimpleClientRegionOverrideFixesEmptyEnvRegion(t *testing.T) {
	defer setenvForTest(t, "VOLCENGINE_ACCESS_KEY", "env-ak")()
	defer setenvForTest(t, "VOLCENGINE_SECRET_KEY", "env-sk")()
	defer setenvForTest(t, "VOLCENGINE_REGION", "")()

	ctx := NewContext()
	ctx.config = &Configure{
		Current:  "",
		Profiles: map[string]*Profile{},
	}

	f, _ := ctx.fixedFlags.AddByName("region")
	f.SetValue("ap-southeast-1")

	client, err := NewSimpleClient(ctx)
	if err != nil {
		t.Fatalf("expected ---region to override empty env region, got error: %v", err)
	}
	if *client.Config.Region != "ap-southeast-1" {
		t.Fatalf("expected region ap-southeast-1, got %q", *client.Config.Region)
	}
}

func TestNewSimpleClientNilConfig(t *testing.T) {
	defer setenvForTest(t, "VOLCENGINE_ACCESS_KEY", "env-ak")()
	defer setenvForTest(t, "VOLCENGINE_SECRET_KEY", "env-sk")()
	defer setenvForTest(t, "VOLCENGINE_REGION", "cn-beijing")()

	ctx := NewContext()
	// config 为 nil，应该走默认凭证链
	ctx.config = nil

	client, err := NewSimpleClient(ctx)
	if err != nil {
		t.Fatalf("expected nil config to use default chain, got error: %v", err)
	}
	if client == nil {
		t.Fatal("expected non-nil client")
	}
}

// --------------- SSO writeback with ---profile override ---------------

func TestEnsureValidStsTokenWritesToCorrectProfile(t *testing.T) {
	// 模拟 ---profile 指向 sso-prod，而 ctx.config.Current 是 default
	// EnsureValidStsToken 应该把凭证写回 sso-prod，不是 default
	disableSSL := false
	cfg := &Configure{
		Current: "default",
		Profiles: map[string]*Profile{
			"default": {
				Name:       "default",
				Mode:       ModeAK,
				AccessKey:  "default-ak",
				SecretKey:  "default-sk",
				Region:     "cn-beijing",
				DisableSSL: &disableSSL,
			},
			"sso-prod": {
				Name:           "sso-prod",
				Mode:           ModeSSO,
				Region:         "cn-beijing",
				SsoSessionName: "my-session",
				AccountId:      "2100000000",
				RoleName:       "MyRole",
				AccessKey:      "old-ak",
				SecretKey:      "old-sk",
				SessionToken:   "old-token",
				StsExpiration:  0, // 已过期，触发刷新
				DisableSSL:     &disableSSL,
			},
		},
	}

	ssoProfile := cfg.Profiles["sso-prod"]

	// 直接测试写回目标：修改 profile 后写入 config
	ssoProfile.AccessKey = "new-ak"
	ssoProfile.SecretKey = "new-sk"
	ssoProfile.SessionToken = "new-token"
	ssoProfile.StsExpiration = 9999999999

	// 模拟 EnsureValidStsToken 的写回逻辑（使用 profile.Name 而不是 config.Current）
	cfg.Profiles[ssoProfile.Name] = ssoProfile

	// 验证 default profile 没有被污染
	defaultProfile := cfg.Profiles["default"]
	if defaultProfile.AccessKey != "default-ak" {
		t.Fatalf("default profile should not be modified, got AccessKey=%q", defaultProfile.AccessKey)
	}
	if defaultProfile.SessionToken != "" {
		t.Fatalf("default profile should not have session token, got %q", defaultProfile.SessionToken)
	}

	// 验证 sso-prod profile 被正确更新
	updatedProfile := cfg.Profiles["sso-prod"]
	if updatedProfile.AccessKey != "new-ak" {
		t.Fatalf("sso-prod profile should be updated, got AccessKey=%q", updatedProfile.AccessKey)
	}
	if updatedProfile.SessionToken != "new-token" {
		t.Fatalf("sso-prod profile should have new token, got %q", updatedProfile.SessionToken)
	}
}

// --------------- SDK CliProvider contract tests ---------------

// 验证 SDK CliProvider 能正确读取 CLI 写入的 config.json 各模式
func writeTestConfig(t *testing.T, cfg *Configure) (string, func()) {
	t.Helper()
	dir := tempDirForTest(t)
	configDir := filepath.Join(dir, ".volcengine")
	if err := os.MkdirAll(configDir, 0700); err != nil {
		t.Fatalf("failed to create config dir: %v", err)
	}
	configPath := filepath.Join(configDir, "config.json")

	data, err := marshalConfig(cfg)
	if err != nil {
		t.Fatalf("marshalConfig error: %v", err)
	}
	if err := ioutil.WriteFile(configPath, data, 0600); err != nil {
		t.Fatalf("failed to write config: %v", err)
	}
	return configPath, cleanupDirForTest(dir)
}

func TestCliProviderContractAkMode(t *testing.T) {
	configPath, cleanup := writeTestConfig(t, &Configure{
		Current: "test",
		Profiles: map[string]*Profile{
			"test": {
				Name:      "test",
				Mode:      ModeAK,
				AccessKey: "test-ak",
				SecretKey: "test-sk",
				Region:    "cn-beijing",
			},
		},
	})
	defer cleanup()

	creds := clicreds.NewCliCredentials(configPath, "test")
	v, err := creds.Get()
	if err != nil {
		t.Fatalf("expected CliProvider to resolve ak mode, got error: %v", err)
	}
	if v.AccessKeyID != "test-ak" {
		t.Fatalf("expected AccessKeyID=test-ak, got %q", v.AccessKeyID)
	}
	if v.SecretAccessKey != "test-sk" {
		t.Fatalf("expected SecretAccessKey=test-sk, got %q", v.SecretAccessKey)
	}
}

func TestCliProviderContractProfileSelection(t *testing.T) {
	configPath, cleanup := writeTestConfig(t, &Configure{
		Current: "default",
		Profiles: map[string]*Profile{
			"default": {
				Name:      "default",
				Mode:      ModeAK,
				AccessKey: "default-ak",
				SecretKey: "default-sk",
			},
			"prod": {
				Name:      "prod",
				Mode:      ModeAK,
				AccessKey: "prod-ak",
				SecretKey: "prod-sk",
			},
		},
	})
	defer cleanup()

	// 指定 profile=prod，应该读 prod 的凭证
	creds := clicreds.NewCliCredentials(configPath, "prod")
	v, err := creds.Get()
	if err != nil {
		t.Fatalf("expected CliProvider to resolve prod profile, got error: %v", err)
	}
	if v.AccessKeyID != "prod-ak" {
		t.Fatalf("expected AccessKeyID=prod-ak, got %q", v.AccessKeyID)
	}
}

func TestCliProviderContractProfileNotFound(t *testing.T) {
	configPath, cleanup := writeTestConfig(t, &Configure{
		Current: "default",
		Profiles: map[string]*Profile{
			"default": {
				Name:      "default",
				Mode:      ModeAK,
				AccessKey: "ak",
				SecretKey: "sk",
			},
		},
	})
	defer cleanup()

	creds := clicreds.NewCliCredentials(configPath, "nonexistent")
	_, err := creds.Get()
	if err == nil {
		t.Fatal("expected error when profile does not exist")
	}
}

func TestCliProviderContractUnsupportedMode(t *testing.T) {
	configPath, cleanup := writeTestConfig(t, &Configure{
		Current: "test",
		Profiles: map[string]*Profile{
			"test": {
				Name: "test",
				Mode: "invalid-mode",
			},
		},
	})
	defer cleanup()

	creds := clicreds.NewCliCredentials(configPath, "test")
	_, err := creds.Get()
	if err == nil {
		t.Fatal("expected error for unsupported mode")
	}
}

// --------------- configure set: DisableSSL / UseDualStack 覆盖语义 ---------------
//
// pflag 的 Bool() 始终返回非 nil 指针（默认 false）。如果 RunE 不做处理直接把 profileFlags
// 传给 mergeProfile，"用户没传 flag" 和 "用户显式传 --disable-ssl=false" 在被调函数侧无法
// 区分，会把已有 profile 中显式启用的 DisableSSL/UseDualStack 静默重置为 false。
// 下面三个用例覆盖 set 子命令调用层（newConfigureSetCmd → RunE → setConfigProfile）。

func resetProfileFlagsForTest(t *testing.T) func() {
	t.Helper()
	old := profileFlags
	profileFlags = Profile{}
	return func() { profileFlags = old }
}

func withTestCtxConfig(t *testing.T, cfg *Configure) func() {
	t.Helper()
	old := ctx.config
	ctx.config = cfg
	return func() { ctx.config = old }
}

func TestConfigureSetPreservesPointerFlagsWhenNotPassed(t *testing.T) {
	_, cleanupConfigDir := withTestConfigDir(t)
	defer cleanupConfigDir()
	defer resetProfileFlagsForTest(t)()

	trueVal := true
	defer withTestCtxConfig(t, &Configure{
		Current: "p1",
		Profiles: map[string]*Profile{
			"p1": {
				Name:         "p1",
				Mode:         ModeAK,
				AccessKey:    "old-ak",
				SecretKey:    "old-sk",
				Region:       "cn-beijing",
				DisableSSL:   &trueVal,
				UseDualStack: &trueVal,
			},
		},
	})()

	setCmd := newConfigureSetCmd()
	setCmd.SetArgs([]string{"--profile", "p1", "--region", "cn-shanghai"})
	if err := setCmd.Execute(); err != nil {
		t.Fatalf("set cmd execute: %v", err)
	}

	cfg := LoadConfig()
	if cfg == nil {
		t.Fatal("LoadConfig returned nil")
	}
	p := cfg.Profiles["p1"]
	if p == nil {
		t.Fatal("profile p1 missing after set")
	}
	if p.Region != "cn-shanghai" {
		t.Fatalf("region should be updated, got %q", p.Region)
	}
	if p.DisableSSL == nil || !*p.DisableSSL {
		t.Fatalf("DisableSSL should remain true when --disable-ssl not passed, got %v", p.DisableSSL)
	}
	if p.UseDualStack == nil || !*p.UseDualStack {
		t.Fatalf("UseDualStack should remain true when --use-dual-stack not passed, got %v", p.UseDualStack)
	}
}

func TestConfigureSetExplicitFalseOverridesPointerFlags(t *testing.T) {
	_, cleanupConfigDir := withTestConfigDir(t)
	defer cleanupConfigDir()
	defer resetProfileFlagsForTest(t)()

	trueVal := true
	defer withTestCtxConfig(t, &Configure{
		Current: "p1",
		Profiles: map[string]*Profile{
			"p1": {
				Name:         "p1",
				Mode:         ModeAK,
				AccessKey:    "old-ak",
				SecretKey:    "old-sk",
				Region:       "cn-beijing",
				DisableSSL:   &trueVal,
				UseDualStack: &trueVal,
			},
		},
	})()

	setCmd := newConfigureSetCmd()
	setCmd.SetArgs([]string{
		"--profile", "p1",
		"--disable-ssl=false",
		"--use-dual-stack=false",
	})
	if err := setCmd.Execute(); err != nil {
		t.Fatalf("set cmd execute: %v", err)
	}

	cfg := LoadConfig()
	p := cfg.Profiles["p1"]
	if p == nil {
		t.Fatal("profile p1 missing after set")
	}
	if p.DisableSSL == nil || *p.DisableSSL {
		t.Fatalf("DisableSSL should be explicitly false, got %v", p.DisableSSL)
	}
	if p.UseDualStack == nil || *p.UseDualStack {
		t.Fatalf("UseDualStack should be explicitly false, got %v", p.UseDualStack)
	}
}

func TestConfigureSetInitializesPointerFlagsForNewProfile(t *testing.T) {
	_, cleanupConfigDir := withTestConfigDir(t)
	defer cleanupConfigDir()
	defer resetProfileFlagsForTest(t)()

	defer withTestCtxConfig(t, &Configure{
		Profiles: map[string]*Profile{},
	})()

	setCmd := newConfigureSetCmd()
	setCmd.SetArgs([]string{
		"--profile", "fresh",
		"--region", "cn-beijing",
		"--access-key", "ak",
		"--secret-key", "sk",
	})
	if err := setCmd.Execute(); err != nil {
		t.Fatalf("set cmd execute: %v", err)
	}

	cfg := LoadConfig()
	p := cfg.Profiles["fresh"]
	if p == nil {
		t.Fatal("profile fresh not created")
	}
	if p.DisableSSL == nil || *p.DisableSSL {
		t.Fatalf("new profile DisableSSL should be non-nil false, got %v", p.DisableSSL)
	}
	if p.UseDualStack == nil || *p.UseDualStack {
		t.Fatalf("new profile UseDualStack should be non-nil false, got %v", p.UseDualStack)
	}
}

func TestConfigureSetHelpIncludesCredentialExamples(t *testing.T) {
	defer resetProfileFlagsForTest(t)()

	cmd := newConfigureSetCmd()
	var b strings.Builder
	cmd.SetOut(&b)
	cmd.SetErr(&b)
	cmd.SetArgs([]string{"--help"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("help execute: %v", err)
	}
	out := b.String()
	for _, want := range []string{
		"ve configure set --profile test --region cn-beijing --access-key ak --secret-key sk",
		"ve configure set --profile test-ram --mode ramrolearn",
		"ve configure set --profile test-oidc --mode oidc",
		"ve configure set --profile test-ecs --mode ecsrole",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("help output missing %q:\n%s", want, out)
		}
	}
}

func TestWriteConfigToFileOverwritesExisting(t *testing.T) {
	configDir, cleanup := withTestConfigDir(t)
	defer cleanup()
	if err := os.Chmod(configDir, 0755); err != nil {
		t.Fatalf("make config directory deliberately permissive: %v", err)
	}

	first := &Configure{
		Current:     "one",
		Profiles:    map[string]*Profile{},
		SsoSession:  map[string]*SsoSession{},
		EnableColor: true,
	}
	if err := WriteConfigToFile(first); err != nil {
		t.Fatalf("first WriteConfigToFile: %v", err)
	}
	configPath := filepath.Join(configDir, ConfigFile)
	if err := os.Chmod(configPath, 0644); err != nil {
		t.Fatalf("make existing config deliberately permissive: %v", err)
	}
	// Preserve the exported API's historical whole-file replacement contract:
	// package-external callers cannot populate the private baseline fields.
	second := &Configure{
		Current:     "two",
		Profiles:    map[string]*Profile{},
		SsoSession:  map[string]*SsoSession{},
		EnableColor: false,
	}
	if err := WriteConfigToFile(second); err != nil {
		t.Fatalf("overwrite WriteConfigToFile: %v", err)
	}
	if runtime.GOOS != "windows" {
		assertFilePerm(t, configDir, 0700)
		assertFilePerm(t, configPath, 0600)
	}
	assertNoConfigTempFiles(t, configDir)
	loaded := LoadConfig()
	if loaded == nil || loaded.Current != "two" || loaded.EnableColor {
		t.Fatalf("loaded after overwrite = %#v, want current=two enableColor=false", loaded)
	}
}

func TestConfigForWriteCapturesBaselineAfterLoadConfigEmptyFile(t *testing.T) {
	configDir, cleanup := withTestConfigDir(t)
	defer cleanup()
	oldConfig := config
	oldContextConfig := ctx.config
	config = nil
	ctx.config = nil
	defer func() {
		config = oldConfig
		ctx.config = oldContextConfig
	}()

	// Preserve LoadConfig's historical empty-file behavior. configForWrite must
	// still turn that newly created file into a writable config with a baseline.
	if loaded := LoadConfig(); loaded != nil {
		t.Fatalf("LoadConfig on a new empty file = %#v, want nil", loaded)
	}
	cfg, err := configForWrite()
	if err != nil {
		t.Fatalf("configForWrite after empty LoadConfig: %v", err)
	}
	if cfg.baseline == nil || !sameConfigPath(cfg.baselinePath, filepath.Join(configDir, ConfigFile)) {
		t.Fatalf("configForWrite did not capture empty-file baseline: %#v path=%q", cfg.baseline, cfg.baselinePath)
	}
	cfg.Profiles["new"] = &Profile{Name: "new"}
	if err := WriteConfigToFile(cfg); err != nil {
		t.Fatalf("write config created from empty baseline: %v", err)
	}
	if loaded := LoadConfig(); loaded == nil || loaded.Profiles["new"] == nil {
		t.Fatalf("new config was not persisted: %#v", loaded)
	}
}

func TestWriteConfigTransactionWithoutBaselineCannotOverwriteExistingConfig(t *testing.T) {
	_, cleanup := withTestConfigDir(t)
	defer cleanup()

	first := &Configure{
		Current: "first",
		Profiles: map[string]*Profile{
			"first": {Name: "first"},
		},
		SsoSession: map[string]*SsoSession{},
	}
	if err := WriteConfigToFile(first); err != nil {
		t.Fatalf("write initial config: %v", err)
	}
	unsafeOverwrite := &Configure{
		Current: "second",
		Profiles: map[string]*Profile{
			"second": {Name: "second"},
		},
		SsoSession: map[string]*SsoSession{},
	}
	if err := writeConfigTransaction(unsafeOverwrite); !errors.Is(err, ErrConcurrentConfigModification) {
		t.Fatalf("baseline-free overwrite error = %v, want concurrent modification", err)
	}
	loaded := LoadConfig()
	if loaded == nil || loaded.Current != "first" || loaded.Profiles["first"] == nil {
		t.Fatalf("baseline-free overwrite changed existing config: %#v", loaded)
	}
}

func TestWriteConfigToFileMergesIndependentStaleChanges(t *testing.T) {
	configDir, cleanup := withTestConfigDir(t)
	defer cleanup()

	seed := &Configure{
		Current: "base",
		Profiles: map[string]*Profile{
			"base": {Name: "base", Mode: ModeAK, AccessKey: "ak", SecretKey: "sk"},
		},
		SsoSession: map[string]*SsoSession{},
	}
	if err := WriteConfigToFile(seed); err != nil {
		t.Fatalf("seed config: %v", err)
	}

	profileWriter := LoadConfig()
	sessionWriter := LoadConfig()
	if profileWriter == nil || sessionWriter == nil {
		t.Fatal("LoadConfig returned nil")
	}
	profileWriter.Profiles["profile-from-first-process"] = &Profile{
		Name:      "profile-from-first-process",
		Mode:      ModeAK,
		AccessKey: "first-ak",
		SecretKey: "first-sk",
	}
	profileWriter.SsoSession["session-from-first-process"] = &SsoSession{
		Name:     "session-from-first-process",
		StartURL: "https://first.example.com/start",
		Region:   "cn-beijing",
	}
	sessionWriter.Profiles["profile-from-second-process"] = &Profile{
		Name:      "profile-from-second-process",
		Mode:      ModeAK,
		AccessKey: "second-ak",
		SecretKey: "second-sk",
	}
	sessionWriter.SsoSession["session-from-second-process"] = &SsoSession{
		Name:     "session-from-second-process",
		StartURL: "https://example.com/start",
		Region:   "cn-beijing",
	}

	if err := writeConfigTransaction(profileWriter); err != nil {
		t.Fatalf("write first stale config: %v", err)
	}
	if err := writeConfigTransaction(sessionWriter); err != nil {
		t.Fatalf("merge second stale config: %v", err)
	}

	loaded := LoadConfig()
	if loaded == nil {
		t.Fatal("LoadConfig after merged writes returned nil")
	}
	if loaded.Profiles["profile-from-first-process"] == nil {
		t.Fatalf("first process profile was lost: %#v", loaded.Profiles)
	}
	if loaded.Profiles["profile-from-second-process"] == nil {
		t.Fatalf("second process profile was lost: %#v", loaded.Profiles)
	}
	if loaded.SsoSession["session-from-first-process"] == nil {
		t.Fatalf("first process SSO session was lost: %#v", loaded.SsoSession)
	}
	if loaded.SsoSession["session-from-second-process"] == nil {
		t.Fatalf("second process SSO session was lost: %#v", loaded.SsoSession)
	}
	if sessionWriter.Profiles["profile-from-first-process"] == nil {
		t.Fatal("successful merge did not refresh the caller's in-memory config")
	}
	if sessionWriter.baseline == nil || !configDataEqual(sessionWriter.baseline, sessionWriter) {
		t.Fatal("successful merge did not refresh the caller's immutable baseline")
	}
	if _, err := os.Stat(filepath.Join(configDir, ConfigFile+".lock")); err != nil {
		t.Fatalf("stable config lock file missing after write: %v", err)
	}
}

func TestWriteConfigToFileRejectsSameKeyStaleConflictAndPreservesFirstWrite(t *testing.T) {
	configDir, cleanup := withTestConfigDir(t)
	defer cleanup()

	seed := &Configure{
		Current: "shared",
		Profiles: map[string]*Profile{
			"shared": {
				Name:      "shared",
				Mode:      ModeAK,
				AccessKey: "base-ak",
				SecretKey: "base-sk",
			},
		},
		SsoSession: map[string]*SsoSession{},
	}
	if err := WriteConfigToFile(seed); err != nil {
		t.Fatalf("seed config: %v", err)
	}

	first := LoadConfig()
	stale := LoadConfig()
	first.Profiles["shared"].Region = "cn-beijing"
	stale.Profiles["shared"].Region = "cn-shanghai"

	if err := writeConfigTransaction(first); err != nil {
		t.Fatalf("write first change: %v", err)
	}
	err := writeConfigTransaction(stale)
	if !errors.Is(err, ErrConcurrentConfigModification) {
		t.Fatalf("stale same-key write error = %v, want concurrent modification", err)
	}
	if !strings.Contains(err.Error(), `profiles["shared"]`) {
		t.Fatalf("conflict error does not identify profile key: %v", err)
	}

	loaded := LoadConfig()
	if loaded == nil || loaded.Profiles["shared"] == nil {
		t.Fatalf("config missing after conflict: %#v", loaded)
	}
	if got := loaded.Profiles["shared"].Region; got != "cn-beijing" {
		t.Fatalf("conflicting stale write changed disk region to %q; want first writer's value", got)
	}
	if _, err := os.Stat(filepath.Join(configDir, ConfigFile+".lock")); err != nil {
		t.Fatalf("config lock file should remain after conflict: %v", err)
	}
}

func TestWriteConfigToFileRejectsConcurrentCurrentChange(t *testing.T) {
	_, cleanup := withTestConfigDir(t)
	defer cleanup()

	seed := &Configure{
		Current: "base",
		Profiles: map[string]*Profile{
			"base":  {Name: "base"},
			"first": {Name: "first"},
			"stale": {Name: "stale"},
		},
		SsoSession: map[string]*SsoSession{},
	}
	if err := WriteConfigToFile(seed); err != nil {
		t.Fatalf("seed config: %v", err)
	}
	first := LoadConfig()
	stale := LoadConfig()
	first.Current = "first"
	stale.Current = "stale"

	if err := writeConfigTransaction(first); err != nil {
		t.Fatalf("write first current: %v", err)
	}
	if err := writeConfigTransaction(stale); !errors.Is(err, ErrConcurrentConfigModification) {
		t.Fatalf("stale current write error = %v, want concurrent modification", err)
	}
	loaded := LoadConfig()
	if loaded == nil || loaded.Current != "first" {
		t.Fatalf("disk current after conflict = %#v, want first", loaded)
	}
}

func TestMergeConfigAcceptsConcurrentSameEnableColorChange(t *testing.T) {
	base := &Configure{EnableColor: false}
	local := &Configure{EnableColor: true}
	remote := &Configure{EnableColor: true}

	merged, err := mergeConfig(base, local, remote)
	if err != nil {
		t.Fatalf("same final enableColor should converge: %v", err)
	}
	if !merged.EnableColor {
		t.Fatal("merged enableColor = false, want true")
	}
}

func TestMergeConfigAcceptsConcurrentSameMapEntryChanges(t *testing.T) {
	base := &Configure{
		Profiles: map[string]*Profile{"shared": {Name: "shared", Region: "base"}},
		SsoSession: map[string]*SsoSession{
			"shared": {Name: "shared", Region: "base"},
		},
	}
	local := normalizedConfigCopy(base)
	remote := normalizedConfigCopy(base)
	local.Profiles["shared"].Region = "same"
	remote.Profiles["shared"].Region = "same"
	local.SsoSession["shared"].Region = "same"
	remote.SsoSession["shared"].Region = "same"

	merged, err := mergeConfig(base, local, remote)
	if err != nil {
		t.Fatalf("same final map-entry values should converge: %v", err)
	}
	if merged.Profiles["shared"].Region != "same" ||
		merged.SsoSession["shared"].Region != "same" {
		t.Fatalf("same-final merge lost values: %#v", merged)
	}
}

func TestWriteConfigTransactionAdvancesStateAfterPartialCommit(t *testing.T) {
	configDir, cleanup := withTestConfigDir(t)
	defer cleanup()

	seed := &Configure{
		Current:    "base",
		Profiles:   map[string]*Profile{"base": {Name: "base"}},
		SsoSession: map[string]*SsoSession{},
	}
	if err := WriteConfigToFile(seed); err != nil {
		t.Fatal(err)
	}
	cfg := LoadConfig()
	cfg.Profiles["new"] = &Profile{Name: "new"}

	durabilityErr := errors.New("injected parent sync failure")
	replacer := func(src, dst string) error {
		if err := os.Rename(src, dst); err != nil {
			return err
		}
		return &PartialCommitError{Err: durabilityErr}
	}
	err := writeConfigTransactionWithReplacer(cfg, replacer)
	var partial *PartialCommitError
	if !errors.As(err, &partial) || !partial.Committed() || !errors.Is(err, durabilityErr) {
		t.Fatalf("partial commit error = %v", err)
	}
	if cfg.Profiles["new"] == nil || cfg.baseline == nil || cfg.baseline.Profiles["new"] == nil {
		t.Fatalf("committed object/baseline not advanced: cfg=%#v baseline=%#v", cfg, cfg.baseline)
	}
	loaded := LoadConfig()
	if loaded == nil || loaded.Profiles["new"] == nil {
		t.Fatalf("partial commit not visible on disk: %#v", loaded)
	}
	// Retrying the now-converged transaction must not report a stale conflict.
	if err := writeConfigTransaction(cfg); err != nil {
		t.Fatalf("retry after partial commit: %v", err)
	}
	assertNoConfigTempFiles(t, configDir)
}

func TestWriteConfigTransactionHardFailureRestoresBaseline(t *testing.T) {
	_, cleanup := withTestConfigDir(t)
	defer cleanup()
	seed := &Configure{Current: "base", Profiles: map[string]*Profile{"base": {Name: "base"}}, SsoSession: map[string]*SsoSession{}}
	if err := WriteConfigToFile(seed); err != nil {
		t.Fatal(err)
	}
	cfg := LoadConfig()
	cfg.Current = "mutated"
	cfg.Profiles["new"] = &Profile{Name: "new"}
	wantErr := errors.New("injected hard replace failure")
	err := writeConfigTransactionWithReplacer(cfg, func(string, string) error { return wantErr })
	if !errors.Is(err, wantErr) {
		t.Fatalf("transaction error = %v, want hard failure", err)
	}
	if cfg.Current != "base" || cfg.Profiles["new"] != nil {
		t.Fatalf("hard failure left uncommitted in-memory state: %#v", cfg)
	}
	if cfg.baseline == nil || cfg.baseline.Current != "base" {
		t.Fatalf("baseline changed after hard failure: %#v", cfg.baseline)
	}
}

func TestWriteConfigToFileRejectsSameSsoSessionKeyConflict(t *testing.T) {
	_, cleanup := withTestConfigDir(t)
	defer cleanup()

	seed := &Configure{
		Profiles: map[string]*Profile{},
		SsoSession: map[string]*SsoSession{
			"shared": {Name: "shared", StartURL: "https://base.example.com", Region: "cn-beijing"},
		},
	}
	if err := WriteConfigToFile(seed); err != nil {
		t.Fatalf("seed config: %v", err)
	}
	first := LoadConfig()
	stale := LoadConfig()
	first.SsoSession["shared"].StartURL = "https://first.example.com"
	stale.SsoSession["shared"].StartURL = "https://stale.example.com"

	if err := writeConfigTransaction(first); err != nil {
		t.Fatalf("write first SSO session update: %v", err)
	}
	err := writeConfigTransaction(stale)
	if !errors.Is(err, ErrConcurrentConfigModification) || !strings.Contains(err.Error(), `sso-session["shared"]`) {
		t.Fatalf("stale SSO session error = %v, want identified concurrent modification", err)
	}
	loaded := LoadConfig()
	if got := loaded.SsoSession["shared"].StartURL; got != "https://first.example.com" {
		t.Fatalf("conflicting stale write changed disk SSO URL to %q", got)
	}
}

func TestConfigFileLockSerializesReadMergeAndReplace(t *testing.T) {
	_, cleanup := withTestConfigDir(t)
	defer cleanup()

	seed := &Configure{
		Profiles:   map[string]*Profile{},
		SsoSession: map[string]*SsoSession{},
	}
	if err := WriteConfigToFile(seed); err != nil {
		t.Fatalf("seed config: %v", err)
	}
	first := LoadConfig()
	second := LoadConfig()
	first.Profiles["first"] = &Profile{Name: "first"}
	second.Profiles["second"] = &Profile{Name: "second"}

	firstInReplace := make(chan struct{})
	releaseFirst := make(chan struct{})
	secondInReplace := make(chan struct{})
	secondStarted := make(chan struct{})
	var closeFirstOnce sync.Once
	firstReplacer := func(src, dst string) error {
		closeFirstOnce.Do(func() { close(firstInReplace) })
		<-releaseFirst
		return replaceFile(src, dst)
	}
	secondReplacer := func(src, dst string) error {
		close(secondInReplace)
		return replaceFile(src, dst)
	}

	firstErr := make(chan error, 1)
	secondErr := make(chan error, 1)
	go func() { firstErr <- writeConfigTransactionWithReplacer(first, firstReplacer) }()
	select {
	case <-firstInReplace:
	case <-time.After(5 * time.Second):
		t.Fatal("first writer did not reach replace")
	}
	go func() {
		close(secondStarted)
		secondErr <- writeConfigTransactionWithReplacer(second, secondReplacer)
	}()
	<-secondStarted

	select {
	case <-secondInReplace:
		close(releaseFirst)
		t.Fatal("second writer reached replace while first held config lock")
	case <-time.After(100 * time.Millisecond):
	}
	close(releaseFirst)
	if err := <-firstErr; err != nil {
		t.Fatalf("first writer: %v", err)
	}
	if err := <-secondErr; err != nil {
		t.Fatalf("second writer: %v", err)
	}

	loaded := LoadConfig()
	if loaded == nil || loaded.Profiles["first"] == nil || loaded.Profiles["second"] == nil {
		t.Fatalf("serialized writes did not preserve both changes: %#v", loaded)
	}
}

func TestWriteConfigToFileReplaceFailurePreservesExistingAndCleansTemp(t *testing.T) {
	configDir, cleanup := withTestConfigDir(t)
	defer cleanup()

	configPath := filepath.Join(configDir, ConfigFile)
	wantOldConfig := &Configure{
		Current:     "existing",
		Profiles:    map[string]*Profile{},
		SsoSession:  map[string]*SsoSession{},
		EnableColor: false,
	}
	if err := WriteConfigToFile(wantOldConfig); err != nil {
		t.Fatalf("write existing config: %v", err)
	}
	wantOld, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("read existing config: %v", err)
	}
	wantNew := LoadConfig()
	if wantNew == nil {
		t.Fatal("LoadConfig before replacement returned nil")
	}
	wantNew.Current = "replacement"
	wantNew.EnableColor = true
	replaceErr := errors.New("injected replace failure")
	var tempPath string
	replacer := func(src, dst string) error {
		tempPath = src
		if dst != configPath {
			t.Fatalf("replacement destination = %q, want %q", dst, configPath)
		}
		if filepath.Dir(src) != configDir {
			t.Fatalf("temporary file directory = %q, want %q", filepath.Dir(src), configDir)
		}

		tempData, err := os.ReadFile(src)
		if err != nil {
			t.Fatalf("temporary source must exist when replacement starts: %v", err)
		}
		expectedData, err := marshalConfig(wantNew)
		if err != nil {
			t.Fatalf("marshal expected config: %v", err)
		}
		if !bytes.Equal(tempData, expectedData) {
			t.Fatalf("temporary source content = %q, want %q", tempData, expectedData)
		}
		if runtime.GOOS != "windows" {
			assertFilePerm(t, src, 0600)
		}

		oldData, err := os.ReadFile(dst)
		if err != nil {
			t.Fatalf("read destination before injected failure: %v", err)
		}
		if !bytes.Equal(oldData, wantOld) {
			t.Fatalf("destination changed before replacement: got %q, want %q", oldData, wantOld)
		}
		return replaceErr
	}

	if err := writeConfigToFile(wantNew, replacer); !errors.Is(err, replaceErr) {
		t.Fatalf("writeConfigToFile error = %v, want injected error", err)
	}
	gotOld, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("existing destination was lost after failed replacement: %v", err)
	}
	if !bytes.Equal(gotOld, wantOld) {
		t.Fatalf("destination changed after failed replacement: got %q, want %q", gotOld, wantOld)
	}
	if tempPath == "" {
		t.Fatal("replacement hook was not called")
	}
	if _, err := os.Stat(tempPath); !os.IsNotExist(err) {
		t.Fatalf("temporary source was not removed after failed replacement: %v", err)
	}
	assertNoConfigTempFiles(t, configDir)
}

func assertFilePerm(t *testing.T, path string, want os.FileMode) {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat %q: %v", path, err)
	}
	if got := info.Mode().Perm(); got != want {
		t.Fatalf("permissions for %q = %04o, want %04o", path, got, want)
	}
}

func assertNoConfigTempFiles(t *testing.T, configDir string) {
	t.Helper()
	matches, err := filepath.Glob(filepath.Join(configDir, ".tmp-config-*"))
	if err != nil {
		t.Fatalf("glob config temporary files: %v", err)
	}
	if len(matches) != 0 {
		t.Fatalf("config temporary files were not cleaned up: %v", matches)
	}
}

func TestReplaceFileOverwritesExisting(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "replacement")
	dst := filepath.Join(dir, "config.json")
	if err := os.WriteFile(src, []byte("new config"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dst, []byte("old config"), 0600); err != nil {
		t.Fatal(err)
	}

	if err := replaceFile(src, dst); err != nil {
		t.Fatalf("replaceFile: %v", err)
	}
	got, err := os.ReadFile(dst)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "new config" {
		t.Fatalf("destination content = %q, want replacement content", got)
	}
}

func TestReplaceFileFailurePreservesExisting(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "missing-replacement")
	dst := filepath.Join(dir, "config.json")
	want := []byte("existing config must survive")
	if err := os.WriteFile(dst, want, 0600); err != nil {
		t.Fatal(err)
	}

	if err := replaceFile(src, dst); err == nil {
		t.Fatal("replaceFile with a missing source unexpectedly succeeded")
	}
	got, err := os.ReadFile(dst)
	if err != nil {
		t.Fatalf("existing destination was lost after failed replacement: %v", err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("destination changed after failed replacement: got %q, want %q", got, want)
	}
}

func TestConfigureSsoSessionHelpIncludesExample(t *testing.T) {
	cmd := newConfigureSsoSessionCmd()
	var b strings.Builder
	cmd.SetOut(&b)
	cmd.SetErr(&b)
	cmd.SetArgs([]string{"--help"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("help execute: %v", err)
	}
	if !strings.Contains(b.String(), "ve configure sso-session --name my-sso") {
		t.Fatalf("help output missing sso-session example:\n%s", b.String())
	}
}
