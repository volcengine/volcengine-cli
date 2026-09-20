package cmd

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/volcengine/volcengine-cli/internal/safety"
)

func TestMCPInvokerSafetyPolicyRequiresExplicitApproval(t *testing.T) {
	dir, cleanup := withTestConfigDir(t)
	defer cleanup()
	if err := safety.SavePolicy(dir, &safety.Policy{
		Enabled: true,
		Rules:   []safety.Rule{{Pattern: "sts:GetCallerIdentity", Action: safety.ActionConfirm}},
	}); err != nil {
		t.Fatalf("save safety policy: %v", err)
	}

	invoker, err := newVolcengineMCPInvoker(&Configure{Profiles: map[string]*Profile{}}, "", "cn-beijing")
	if err != nil {
		t.Fatalf("newVolcengineMCPInvoker() error = %v", err)
	}
	_, err = invoker.CallAPI(context.Background(), "sts", "GetCallerIdentity", "", map[string]interface{}{}, false)
	if err == nil || !strings.Contains(err.Error(), "yes=true") {
		t.Fatalf("unapproved MCP call error = %v", err)
	}
}

func TestMCPInvokerSafetyPolicyDenyCannotBeApproved(t *testing.T) {
	dir, cleanup := withTestConfigDir(t)
	defer cleanup()
	if err := safety.SavePolicy(dir, &safety.Policy{
		Enabled: true,
		Rules:   []safety.Rule{{Pattern: "sts:GetCallerIdentity", Action: safety.ActionDeny}},
	}); err != nil {
		t.Fatalf("save safety policy: %v", err)
	}

	invoker, err := newVolcengineMCPInvoker(&Configure{Profiles: map[string]*Profile{}}, "", "cn-beijing")
	if err != nil {
		t.Fatalf("newVolcengineMCPInvoker() error = %v", err)
	}
	_, err = invoker.CallAPI(context.Background(), "sts", "GetCallerIdentity", "", map[string]interface{}{}, true)
	if err == nil || !strings.Contains(err.Error(), "blocked by safety policy") {
		t.Fatalf("approved denied MCP call error = %v", err)
	}
}

func TestMCPInvokerReloadsProfileConfigurationAfterStartup(t *testing.T) {
	_, cleanup := withTestConfigDir(t)
	defer cleanup()

	initial := &Configure{
		Current: "default",
		Profiles: map[string]*Profile{
			"default": {Name: "default", Mode: ModeConsoleLogin, Region: "cn-beijing"},
		},
	}
	if err := WriteConfigToFile(initial); err != nil {
		t.Fatalf("write initial config: %v", err)
	}
	invoker, err := newReloadingVolcengineMCPInvoker(initial, "", "")
	if err != nil {
		t.Fatalf("newReloadingVolcengineMCPInvoker() error = %v", err)
	}

	updated := &Configure{
		Current: "default",
		Profiles: map[string]*Profile{
			"default": {
				Name: "default", Mode: ModeConsoleLogin, Region: "cn-shanghai", LoginSession: "new-login-session",
			},
		},
	}
	if err := WriteConfigToFile(updated); err != nil {
		t.Fatalf("write updated config: %v", err)
	}

	cfg, profileName, profileRegion, err := invoker.resolveConfig()
	if err != nil {
		t.Fatalf("resolveConfig() error = %v", err)
	}
	if profileName != "default" || profileRegion != "cn-shanghai" {
		t.Fatalf("resolved profile = %q/%q, want default/cn-shanghai", profileName, profileRegion)
	}
	if got := cfg.Profiles[profileName].LoginSession; got != "new-login-session" {
		t.Fatalf("login session = %q, want new-login-session", got)
	}
}

func TestMCPInvokerFallsBackToHeaderCredentials(t *testing.T) {
	requests := make(chan *http.Request, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests <- r.Clone(r.Context())
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ResponseMetadata":{"RequestId":"req-mcp","Action":"GetCallerIdentity","Version":"2018-01-01","Service":"sts","Region":"ap-southeast-1"},"Result":{"AccountId":"test"}}`))
	}))
	defer server.Close()

	for _, name := range []string{
		"VOLCENGINE_ACCESS_KEY", "VOLCENGINE_SECRET_KEY", "VOLCENGINE_SESSION_TOKEN",
		"VOLCENGINE_PROFILE", "VOLCSTACK_PROFILE", "VOLCENGINE_REGION",
	} {
		defer unsetenvForTest(t, name)()
	}
	defer setenvForTest(t, "VOLCENGINE_DISABLE_DEFAULT_CREDENTIALS", "true")()
	defer setenvForTest(t, "VOLCENGINE_ENDPOINT", server.URL)()

	invoker, err := newVolcengineMCPInvoker(&Configure{Profiles: map[string]*Profile{}}, "", "cn-shanghai")
	if err != nil {
		t.Fatalf("newVolcengineMCPInvoker() error = %v", err)
	}
	metadata := mcpRequestMetadata{
		Credentials: &mcpCredentials{
			AccessKeyID: "header-ak", SecretAccessKey: "header-sk", SessionToken: "header-token",
		},
	}
	ctx := context.WithValue(context.Background(), mcpRequestMetadataContextKey{}, metadata)
	if _, err := invoker.CallAPI(ctx, "sts", "GetCallerIdentity", "ap-southeast-1", map[string]interface{}{}, false); err != nil {
		t.Fatalf("CallAPI() error = %v", err)
	}

	request := <-requests
	authorization := request.Header.Get("Authorization")
	if !strings.Contains(authorization, "Credential=header-ak/") || !strings.Contains(authorization, "/ap-southeast-1/sts/request") {
		t.Fatalf("unexpected Authorization header: %q", authorization)
	}
	if token := request.Header.Get("X-Security-Token"); token != "header-token" {
		t.Fatalf("X-Security-Token = %q", token)
	}
}

func TestMCPAPICallHonorsContextCancellation(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(started)
		select {
		case <-r.Context().Done():
		case <-release:
		}
	}))
	defer server.Close()
	defer close(release)

	defer setenvForTest(t, "VOLCENGINE_ACCESS_KEY", "ak-test")()
	defer setenvForTest(t, "VOLCENGINE_SECRET_KEY", "sk-test")()
	defer setenvForTest(t, "VOLCENGINE_REGION", "cn-beijing")()
	callContext := NewContext()
	endpointFlag, _ := callContext.fixedFlags.AddByName("endpoint")
	endpointFlag.SetValue(server.URL)
	sdk, err := NewSimpleClient(callContext)
	if err != nil {
		t.Fatalf("NewSimpleClient() error = %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan error, 1)
	go func() {
		_, err := callAPIWithClientContext(ctx, sdk, "sts", "GetCallerIdentity", map[string]interface{}{}, false)
		result <- err
	}()
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for API request")
	}
	cancel()
	select {
	case err := <-result:
		if err == nil {
			t.Fatal("expected the canceled API request to return an error")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("API request did not stop after context cancellation")
	}
}

func TestMCPInvokerRejectsMissingExplicitProfile(t *testing.T) {
	_, err := newVolcengineMCPInvoker(&Configure{Profiles: map[string]*Profile{}}, "missing", "")
	if err == nil {
		t.Fatal("expected missing explicit profile to fail")
	}
}

func TestMCPInvokerUsesDefaultChainBeforeHeaderCredentials(t *testing.T) {
	requests := make(chan *http.Request, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests <- r.Clone(r.Context())
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ResponseMetadata":{"RequestId":"req-mcp","Action":"GetCallerIdentity","Version":"2018-01-01","Service":"sts","Region":"cn-beijing"},"Result":{}}`))
	}))
	defer server.Close()

	defer setenvForTest(t, "VOLCENGINE_ACCESS_KEY", "default-ak")()
	defer setenvForTest(t, "VOLCENGINE_SECRET_KEY", "default-sk")()
	defer setenvForTest(t, "VOLCENGINE_REGION", "cn-beijing")()
	defer setenvForTest(t, "VOLCENGINE_ENDPOINT", server.URL)()
	defer unsetenvForTest(t, "VOLCENGINE_DISABLE_DEFAULT_CREDENTIALS")()
	defer unsetenvForTest(t, "VOLCENGINE_PROFILE")()
	defer unsetenvForTest(t, "VOLCSTACK_PROFILE")()

	invoker, err := newVolcengineMCPInvoker(&Configure{Profiles: map[string]*Profile{}}, "", "")
	if err != nil {
		t.Fatalf("newVolcengineMCPInvoker() error = %v", err)
	}
	metadata := mcpRequestMetadata{Credentials: &mcpCredentials{AccessKeyID: "header-ak", SecretAccessKey: "header-sk"}}
	ctx := context.WithValue(context.Background(), mcpRequestMetadataContextKey{}, metadata)
	if _, err := invoker.CallAPI(ctx, "sts", "GetCallerIdentity", "", map[string]interface{}{}, false); err != nil {
		t.Fatalf("CallAPI() error = %v", err)
	}

	authorization := (<-requests).Header.Get("Authorization")
	if !strings.Contains(authorization, "Credential=default-ak/") {
		t.Fatalf("default-chain credential was not used: %q", authorization)
	}
	if strings.Contains(authorization, "header-ak") {
		t.Fatalf("header credential unexpectedly overrode the default chain: %q", authorization)
	}
}

func TestMCPInvokerSelectsCurrentAndExplicitProfiles(t *testing.T) {
	defer unsetenvForTest(t, "VOLCENGINE_PROFILE")()
	defer unsetenvForTest(t, "VOLCSTACK_PROFILE")()
	cfg := &Configure{
		Current: "current",
		Profiles: map[string]*Profile{
			"current":  {Name: "current", Region: "cn-beijing"},
			"explicit": {Name: "explicit", Region: "cn-shanghai"},
		},
	}
	current, err := newVolcengineMCPInvoker(cfg, "", "")
	if err != nil {
		t.Fatalf("create current-profile invoker: %v", err)
	}
	_, currentProfile, _, err := current.resolveConfig()
	if err != nil || currentProfile != "current" {
		t.Fatalf("current profile selection = %q, error = %v", currentProfile, err)
	}
	explicit, err := newVolcengineMCPInvoker(cfg, "explicit", "")
	if err != nil {
		t.Fatalf("create explicit-profile invoker: %v", err)
	}
	_, explicitProfile, _, err := explicit.resolveConfig()
	if err != nil || explicitProfile != "explicit" {
		t.Fatalf("explicit profile selection = %q, error = %v", explicitProfile, err)
	}
}
