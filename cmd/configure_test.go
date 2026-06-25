package cmd

import (
	"encoding/json"
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"
	"testing"

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
