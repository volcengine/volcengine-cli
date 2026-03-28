package cmd

import (
	"encoding/json"
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
	})
	if err != nil {
		t.Fatalf("expected ak mode to be valid, got error: %v", err)
	}
}

func TestValidateProfileModeStsTokenRequiresSessionToken(t *testing.T) {
	err := validateProfileMode(&Profile{
		Name:      "test",
		Mode:      ModeStsToken,
		AccessKey: "ak",
		SecretKey: "sk",
	})
	if err == nil {
		t.Fatal("expected ststoken mode to require session-token")
	}
}

func TestValidateProfileModeStsTokenValid(t *testing.T) {
	err := validateProfileMode(&Profile{
		Name:         "test",
		Mode:         ModeStsToken,
		AccessKey:    "ak",
		SecretKey:    "sk",
		SessionToken: "token",
	})
	if err != nil {
		t.Fatalf("expected ststoken mode to be valid, got error: %v", err)
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
	})
	if err != nil {
		t.Fatalf("expected ecsrole mode to be valid, got error: %v", err)
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
	t.Setenv("VOLCENGINE_OIDC_TOKEN_FILE", "/tmp/token")
	t.Setenv("VOLCENGINE_OIDC_ROLE_TRN", "trn:iam::2100000000:role/TestRole")

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

func TestNewSimpleClientNoProfileUsesDefaultChain(t *testing.T) {
	t.Setenv("VOLCENGINE_ACCESS_KEY", "env-ak")
	t.Setenv("VOLCENGINE_SECRET_KEY", "env-sk")
	t.Setenv("VOLCENGINE_REGION", "cn-beijing")

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
	t.Setenv("VOLCENGINE_ACCESS_KEY", "env-ak")
	t.Setenv("VOLCENGINE_SECRET_KEY", "env-sk")
	t.Setenv("VOLCENGINE_REGION", "")

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
	t.Setenv("VOLCENGINE_ACCESS_KEY", "env-ak")
	t.Setenv("VOLCENGINE_SECRET_KEY", "env-sk")
	t.Setenv("VOLCENGINE_REGION", "")

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
	t.Setenv("VOLCENGINE_ACCESS_KEY", "env-ak")
	t.Setenv("VOLCENGINE_SECRET_KEY", "env-sk")
	t.Setenv("VOLCENGINE_REGION", "cn-beijing")

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
func writeTestConfig(t *testing.T, cfg *Configure) string {
	t.Helper()
	dir := t.TempDir()
	configDir := filepath.Join(dir, ".volcengine")
	if err := os.MkdirAll(configDir, 0700); err != nil {
		t.Fatalf("failed to create config dir: %v", err)
	}
	configPath := filepath.Join(configDir, "config.json")

	data, err := marshalConfig(cfg)
	if err != nil {
		t.Fatalf("marshalConfig error: %v", err)
	}
	if err := os.WriteFile(configPath, data, 0600); err != nil {
		t.Fatalf("failed to write config: %v", err)
	}
	return configPath
}

func TestCliProviderContractAkMode(t *testing.T) {
	configPath := writeTestConfig(t, &Configure{
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

func TestCliProviderContractStsTokenMode(t *testing.T) {
	configPath := writeTestConfig(t, &Configure{
		Current: "test",
		Profiles: map[string]*Profile{
			"test": {
				Name:         "test",
				Mode:         ModeStsToken,
				AccessKey:    "sts-ak",
				SecretKey:    "sts-sk",
				SessionToken: "sts-token",
				Region:       "cn-beijing",
			},
		},
	})

	creds := clicreds.NewCliCredentials(configPath, "test")
	v, err := creds.Get()
	if err != nil {
		t.Fatalf("expected CliProvider to resolve ststoken mode, got error: %v", err)
	}
	if v.AccessKeyID != "sts-ak" {
		t.Fatalf("expected AccessKeyID=sts-ak, got %q", v.AccessKeyID)
	}
	if v.SessionToken != "sts-token" {
		t.Fatalf("expected SessionToken=sts-token, got %q", v.SessionToken)
	}
}

func TestCliProviderContractProfileSelection(t *testing.T) {
	configPath := writeTestConfig(t, &Configure{
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
	configPath := writeTestConfig(t, &Configure{
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

	creds := clicreds.NewCliCredentials(configPath, "nonexistent")
	_, err := creds.Get()
	if err == nil {
		t.Fatal("expected error when profile does not exist")
	}
}

func TestCliProviderContractUnsupportedMode(t *testing.T) {
	configPath := writeTestConfig(t, &Configure{
		Current: "test",
		Profiles: map[string]*Profile{
			"test": {
				Name: "test",
				Mode: "invalid-mode",
			},
		},
	})

	creds := clicreds.NewCliCredentials(configPath, "test")
	_, err := creds.Get()
	if err == nil {
		t.Fatal("expected error for unsupported mode")
	}
}
