package cmd

import (
	"bytes"
	"strings"
	"testing"
)

func TestNewSimpleClientWritesCliDebugSummary(t *testing.T) {
	disableSSL := false
	ctx := NewContext()
	ctx.config = &Configure{
		Current: "default",
		Profiles: map[string]*Profile{
			"default": {
				Name:       "default",
				Mode:       ModeAK,
				AccessKey:  "ak-should-not-leak",
				SecretKey:  "sk-should-not-leak",
				Region:     "cn-beijing",
				Endpoint:   "sts.volcengineapi.com",
				DisableSSL: &disableSSL,
			},
		},
	}
	var out bytes.Buffer
	ctx.debugLogger = &DebugLogger{enabled: true, out: &out}

	if _, err := NewSimpleClient(ctx); err != nil {
		t.Fatalf("NewSimpleClient returned error: %v", err)
	}

	logs := out.String()
	for _, want := range []string{
		"profile_source=current",
		"profile=default",
		"credential_mode=ak",
		"region=cn-beijing",
		"endpoint=sts.volcengineapi.com",
	} {
		if !strings.Contains(logs, want) {
			t.Fatalf("debug logs missing %q:\n%s", want, logs)
		}
	}
	if strings.Contains(logs, "ak-should-not-leak") || strings.Contains(logs, "sk-should-not-leak") {
		t.Fatalf("debug logs leaked credentials:\n%s", logs)
	}
}
