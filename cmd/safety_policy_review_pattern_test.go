package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/volcengine/volcengine-cli/internal/safety"
)

func TestReviewSafetyPolicyPatternSyntax(t *testing.T) {
	for _, name := range []string{"VOLCENGINE_PROFILE", "VOLCSTACK_PROFILE", "VOLCENGINE_ENDPOINT_RESOLVER"} {
		t.Cleanup(unsetenvForTest(t, name))
	}
	t.Setenv("VOLCENGINE_DISABLE_DEFAULT_CREDENTIALS", "true")
	var requests int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&requests, 1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"ResponseMetadata":{"RequestId":"review-local"},"Result":{}}`)
	}))
	defer server.Close()
	t.Setenv("VOLCENGINE_ENDPOINT", server.URL)
	callContext := context.WithValue(context.Background(), mcpRequestMetadataContextKey{}, mcpRequestMetadata{
		Credentials: &mcpCredentials{AccessKeyID: "fake-ak", SecretAccessKey: "fake-sk"},
	})
	for _, tc := range []struct {
		pattern string
		valid   bool
	}{
		{pattern: "GetCallerIdentity"},
		{pattern: "sts.GetCallerIdentity"},
		{pattern: "sts GetCallerIdentity"},
		{pattern: "sts"},
		{pattern: "sts:GetCallerIdentity", valid: true},
		{pattern: "sts:*", valid: true},
		{pattern: "*:GetCallerIdentity", valid: true},
		{pattern: "*:*", valid: true},
	} {
		t.Run(tc.pattern, func(t *testing.T) {
			dir := reviewSafetyEnvironment(t)
			if _, err := runSafetyPolicyCommandForTest(t, "enable"); err != nil {
				t.Fatal(err)
			}
			before, err := os.ReadFile(safety.PolicyFilePath(dir))
			if err != nil {
				t.Fatal(err)
			}
			_, addErr := runSafetyPolicyCommandForTest(t, "add", "--pattern", tc.pattern, "--action", "deny")
			if addErr != nil {
				if tc.valid {
					t.Fatalf("valid pattern rejected: %v", addErr)
				}
				if !strings.Contains(addErr.Error(), "service:action") || !strings.Contains(addErr.Error(), tc.pattern) {
					t.Fatalf("error must identify the pattern and expected syntax: %v", addErr)
				}
				after, err := os.ReadFile(safety.PolicyFilePath(dir))
				if err != nil || !bytes.Equal(before, after) {
					t.Fatalf("invalid add changed stored policy: %v", err)
				}
				t.Logf("pattern=%q rejected: %v; stored policy unchanged", tc.pattern, addErr)
				return
			}
			listed, err := runSafetyPolicyCommandForTest(t, "list")
			if err != nil || !strings.Contains(listed, tc.pattern+" -> deny") {
				t.Fatalf("list: error=%v output=%q", err, listed)
			}
			policy, err := safety.LoadEffectivePolicy(dir)
			if err != nil {
				t.Fatal(err)
			}
			decision := policy.Check(safety.CommandInfo{Product: "sts", Action: "GetCallerIdentity"})
			invoker, err := newVolcengineMCPInvoker(&Configure{Profiles: map[string]*Profile{}}, "", "cn-beijing")
			if err != nil {
				t.Fatal(err)
			}
			atomic.StoreInt32(&requests, 0)
			_, callErr := invoker.CallAPI(callContext, "sts", "GetCallerIdentity", "", map[string]interface{}{}, false)
			count := atomic.LoadInt32(&requests)
			t.Logf("pattern=%q add error=%v listed=true matched=%v decision=%s API error=%v HTTP requests=%d", tc.pattern, addErr, decision.Matched, decision.Action, callErr, count)
			if !tc.valid {
				t.Fatal("malformed pattern was accepted and saved instead of reporting a configuration error")
			}
			if !decision.Matched || decision.Action != safety.ActionDeny || callErr == nil || !strings.Contains(callErr.Error(), "blocked by safety policy") || count != 0 {
				t.Fatal("valid deny pattern must block the API call before HTTP")
			}
		})
	}
}

func TestSafetyMalformedPatternSourcesBlockRequests(t *testing.T) {
	for _, source := range []string{"file", "environment"} {
		t.Run(source, func(t *testing.T) {
			dir := reviewSafetyEnvironment(t)
			for _, name := range []string{"VOLCENGINE_PROFILE", "VOLCSTACK_PROFILE", "VOLCENGINE_ENDPOINT_RESOLVER"} {
				t.Cleanup(unsetenvForTest(t, name))
			}
			t.Setenv("VOLCENGINE_DISABLE_DEFAULT_CREDENTIALS", "true")
			var requests int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				atomic.AddInt32(&requests, 1)
				w.Header().Set("Content-Type", "application/json")
				_, _ = io.WriteString(w, `{"ResponseMetadata":{"RequestId":"review-local"},"Result":{}}`)
			}))
			defer server.Close()
			t.Setenv("VOLCENGINE_ENDPOINT", server.URL)
			policy := &safety.Policy{Enabled: true, Rules: []safety.Rule{{Pattern: "GetCallerIdentity", Action: safety.ActionDeny}}}
			if source == "file" {
				data, err := json.Marshal(policy)
				if err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(safety.PolicyFilePath(dir), data, 0600); err != nil {
					t.Fatal(err)
				}
			} else {
				t.Setenv(safety.EnvPolicyEnabled, "true")
				t.Setenv(safety.EnvPolicyRules, "GetCallerIdentity=deny")
			}
			invoker, err := newVolcengineMCPInvoker(&Configure{Profiles: map[string]*Profile{}}, "", "cn-beijing")
			if err != nil {
				t.Fatal(err)
			}
			callContext := context.WithValue(context.Background(), mcpRequestMetadataContextKey{}, mcpRequestMetadata{
				Credentials: &mcpCredentials{AccessKeyID: "fake-ak", SecretAccessKey: "fake-sk"},
			})
			_, callErr := invoker.CallAPI(callContext, "sts", "GetCallerIdentity", "", map[string]interface{}{}, false)
			count := atomic.LoadInt32(&requests)
			t.Logf("source=%s API error=%v HTTP requests=%d", source, callErr, count)
			if callErr == nil || !strings.Contains(callErr.Error(), "service:action") || count != 0 {
				t.Fatal("malformed policy must block the API before HTTP")
			}
		})
	}
}
