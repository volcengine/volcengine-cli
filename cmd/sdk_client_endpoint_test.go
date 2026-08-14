package cmd

import (
	"strings"
	"testing"
)

func TestClassifyEndpoint(t *testing.T) {
	tests := []struct {
		name     string
		endpoint string
		resolver string
		want     endpointMode
	}{
		{name: "empty", endpoint: "", resolver: "", want: endpointModeSDKDefault},
		{name: "fixed host", endpoint: "open.volcengineapi.com", resolver: "", want: endpointModeFixedHost},
		{name: "standard ignores host", endpoint: "open.volcengineapi.com", resolver: "standard", want: endpointModeStandardResolver},
		{name: "STANDARD case", endpoint: "open.volcengineapi.com", resolver: "STANDARD", want: endpointModeStandardResolver},
		{name: "auto-addressing", endpoint: "auto-addressing", resolver: "", want: endpointModeStandardResolver},
		{name: "Auto-Addressing case", endpoint: "Auto-Addressing", resolver: "", want: endpointModeStandardResolver},
		{name: "standard empty host", endpoint: "", resolver: "standard", want: endpointModeStandardResolver},
		{name: "whitespace host alone", endpoint: "   ", resolver: "", want: endpointModeSDKDefault},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := classifyEndpoint(tt.endpoint, tt.resolver); got != tt.want {
				t.Fatalf("classifyEndpoint(%q, %q) = %v, want %v", tt.endpoint, tt.resolver, got, tt.want)
			}
		})
	}
}

func TestResolveClientEndpointPriority(t *testing.T) {
	defer setenvForTest(t, "VOLCENGINE_ENDPOINT", "env.volcengineapi.com")()
	defer setenvForTest(t, "VOLCENGINE_ENDPOINT_RESOLVER", "")()

	c := NewContext()
	c.config = &Configure{
		Current: "default",
		Profiles: map[string]*Profile{
			"default": {
				Name:             "default",
				Endpoint:         "profile.volcengineapi.com",
				EndpointResolver: "standard",
			},
		},
	}

	// profile resolver/host without flag
	_, _, profile, err := selectInvocationProfile(c)
	if err != nil {
		t.Fatalf("selectInvocationProfile: %v", err)
	}
	ep, res := resolveClientEndpoint(c, profile)
	if ep != "profile.volcengineapi.com" || strings.ToLower(res) != "standard" {
		t.Fatalf("profile sources = (%q, %q), want profile host + standard", ep, res)
	}
	if classifyEndpoint(ep, res) != endpointModeStandardResolver {
		t.Fatal("expected standard mode for profile resolver")
	}

	// ---endpoint clears resolver
	parser := NewParser([]string{"---endpoint", "flag.volcengineapi.com"})
	if _, err := parser.ReadArgs(c); err != nil {
		t.Fatalf("ReadArgs: %v", err)
	}
	ep, res = resolveClientEndpoint(c, profile)
	if ep != "flag.volcengineapi.com" || res != "" {
		t.Fatalf("---endpoint should clear resolver, got (%q, %q)", ep, res)
	}
	if classifyEndpoint(ep, res) != endpointModeFixedHost {
		t.Fatal("expected fixed host after ---endpoint")
	}
}

func TestSelectInvocationProfilePriorityAndMissing(t *testing.T) {
	defer setenvForTest(t, "VOLCENGINE_PROFILE", "")()

	cfg := &Configure{
		Current: "default",
		Profiles: map[string]*Profile{
			"default": {Name: "default"},
			"prod":    {Name: "prod", Endpoint: "prod.volcengineapi.com"},
		},
	}

	c := NewContext()
	c.config = cfg
	name, source, profile, err := selectInvocationProfile(c)
	if err != nil || name != "default" || source != "current" || profile == nil {
		t.Fatalf("current profile: name=%q source=%q profile=%v err=%v", name, source, profile, err)
	}

	c = NewContext()
	c.config = cfg
	parser := NewParser([]string{"---profile", "prod"})
	if _, err := parser.ReadArgs(c); err != nil {
		t.Fatalf("ReadArgs: %v", err)
	}
	name, source, profile, err = selectInvocationProfile(c)
	if err != nil || name != "prod" || source != "flag" || profile == nil || profile.Endpoint != "prod.volcengineapi.com" {
		t.Fatalf("---profile override: name=%q source=%q profile=%v err=%v", name, source, profile, err)
	}

	c = NewContext()
	c.config = cfg
	parser = NewParser([]string{"---profile", "missing"})
	if _, err := parser.ReadArgs(c); err != nil {
		t.Fatalf("ReadArgs: %v", err)
	}
	_, _, _, err = selectInvocationProfile(c)
	if err == nil || !strings.Contains(err.Error(), `profile "missing" not found`) {
		t.Fatalf("expected missing profile error, got: %v", err)
	}
}

func TestHasEffectiveFixedEndpointMatrix(t *testing.T) {
	t.Run("env host", func(t *testing.T) {
		defer setenvForTest(t, "VOLCENGINE_ENDPOINT", "open.volcengineapi.com")()
		defer setenvForTest(t, "VOLCENGINE_ENDPOINT_RESOLVER", "")()
		if !hasEffectiveFixedEndpoint(NewContext()) {
			t.Fatal("expected fixed host from env")
		}
	})
	t.Run("standard resolver alone", func(t *testing.T) {
		defer setenvForTest(t, "VOLCENGINE_ENDPOINT", "open.volcengineapi.com")()
		defer setenvForTest(t, "VOLCENGINE_ENDPOINT_RESOLVER", "standard")()
		if hasEffectiveFixedEndpoint(NewContext()) {
			t.Fatal("standard resolver should not be fixed host")
		}
	})
	t.Run("auto-addressing env", func(t *testing.T) {
		defer setenvForTest(t, "VOLCENGINE_ENDPOINT", "auto-addressing")()
		defer setenvForTest(t, "VOLCENGINE_ENDPOINT_RESOLVER", "")()
		if hasEffectiveFixedEndpoint(NewContext()) {
			t.Fatal("auto-addressing should not be fixed host")
		}
	})
	t.Run("explicit endpoint clears standard", func(t *testing.T) {
		defer setenvForTest(t, "VOLCENGINE_ENDPOINT", "")()
		defer setenvForTest(t, "VOLCENGINE_ENDPOINT_RESOLVER", "standard")()
		c := NewContext()
		parser := NewParser([]string{"---endpoint", "open.volcengineapi.com"})
		if _, err := parser.ReadArgs(c); err != nil {
			t.Fatalf("ReadArgs: %v", err)
		}
		if !hasEffectiveFixedEndpoint(c) {
			t.Fatal("---endpoint should be fixed host")
		}
	})
}
