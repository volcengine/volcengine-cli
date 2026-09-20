package cmd

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"sort"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/volcengine/volcengine-cli/internal/safety"
)

// Run these end-to-end safety checks with the normal cmd test suite.
func reviewSafetyEnvironment(t *testing.T) string {
	t.Helper()
	dir, cleanup := withTestConfigDir(t)
	t.Cleanup(cleanup)
	for _, name := range []string{safety.EnvPolicyEnabled, safety.EnvPolicyRules, safety.EnvSkipConfirm} {
		t.Cleanup(unsetenvForTest(t, name))
	}
	return dir
}

func reviewRunAPICommand(t *testing.T, args ...string) error {
	t.Helper()
	resolved, err := resolveSystemFlags(args)
	if err != nil {
		return err
	}
	previousFixed := processLanguageResolution.fixedFlags
	processLanguageResolution.fixedFlags = resolved.fixedFlags
	defer func() {
		processLanguageResolution.fixedFlags = previousFixed
		rootCmd.SetArgs(nil)
	}()
	rootCmd.SetArgs(resolved.args)
	return rootCmd.Execute()
}

func TestSafetyNullPolicyMustFailClosed(t *testing.T) {
	dir := reviewSafetyEnvironment(t)
	var requests int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&requests, 1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ResponseMetadata":{"RequestId":"review-local"},"Result":{}}`))
	}))
	defer server.Close()
	for _, name := range []string{"VOLCENGINE_ACCESS_KEY", "VOLCENGINE_SECRET_KEY", "VOLCENGINE_SESSION_TOKEN", "VOLCENGINE_PROFILE", "VOLCSTACK_PROFILE", "VOLCENGINE_ENDPOINT_RESOLVER"} {
		t.Cleanup(unsetenvForTest(t, name))
	}
	t.Setenv("VOLCENGINE_DISABLE_DEFAULT_CREDENTIALS", "true")
	t.Setenv("VOLCENGINE_ENDPOINT", server.URL)
	invoker, err := newVolcengineMCPInvoker(&Configure{Profiles: map[string]*Profile{}}, "", "cn-beijing")
	if err != nil {
		t.Fatal(err)
	}
	callContext := context.WithValue(context.Background(), mcpRequestMetadataContextKey{}, mcpRequestMetadata{
		Credentials: &mcpCredentials{AccessKeyID: "fake-ak", SecretAccessKey: "fake-sk"},
	})
	_, err = invoker.CallAPI(callContext, "sts", "GetCallerIdentity", "", map[string]interface{}{}, false)
	if err != nil || atomic.LoadInt32(&requests) != 1 {
		t.Fatalf("allow control: err=%v, HTTP requests=%d", err, atomic.LoadInt32(&requests))
	}
	atomic.StoreInt32(&requests, 0)
	if err := safety.SavePolicy(dir, &safety.Policy{Enabled: true, Rules: []safety.Rule{{Pattern: "sts:*", Action: safety.ActionDeny}}}); err != nil {
		t.Fatal(err)
	}
	_, err = invoker.CallAPI(callContext, "sts", "GetCallerIdentity", "", map[string]interface{}{}, false)
	if err == nil || !strings.Contains(err.Error(), "blocked by safety policy") || atomic.LoadInt32(&requests) != 0 {
		t.Fatalf("deny control: err=%v, requests=%d", err, atomic.LoadInt32(&requests))
	}
	if err := os.WriteFile(safety.PolicyFilePath(dir), []byte("null\n"), 0600); err != nil {
		t.Fatal(err)
	}
	_, err = invoker.CallAPI(callContext, "sts", "GetCallerIdentity", "", map[string]interface{}{}, false)
	if err == nil || !strings.Contains(err.Error(), "safety policy") || atomic.LoadInt32(&requests) != 0 {
		t.Fatalf("null policy must block before HTTP: err=%v, HTTP requests=%d", err, atomic.LoadInt32(&requests))
	}
}

func TestSafetyDevNullMustUseNonInteractiveMessage(t *testing.T) {
	dir := reviewSafetyEnvironment(t)
	if err := safety.SavePolicy(dir, &safety.Policy{Enabled: true, Rules: []safety.Rule{{Pattern: "sts:*", Action: safety.ActionConfirm}}}); err != nil {
		t.Fatal(err)
	}
	input, err := os.Open(os.DevNull)
	if err != nil {
		t.Fatal(err)
	}
	defer input.Close()
	previousInput, previousOutput := safetyPolicyInput, safetyPolicyOutput
	var output bytes.Buffer
	safetyPolicyInput, safetyPolicyOutput = input, &output
	t.Cleanup(func() { safetyPolicyInput, safetyPolicyOutput = previousInput, previousOutput })
	withIsolatedInvocationFlags(t)
	err = reviewRunAPICommand(t, "sts", "GetCallerIdentity")
	if err == nil || !strings.Contains(err.Error(), "non-interactive") || !strings.Contains(err.Error(), "--yes") || output.Len() != 0 {
		t.Fatalf("stdin=/dev/null: err=%v; prompt=%q; want non-interactive error with --yes and no prompt", err, output.String())
	}
}

func TestSafetyMatchesEnteredServiceSpelling(t *testing.T) {
	actions := rootSupport.GetAllAction("rdsmysql")
	sort.Strings(actions)
	if len(actions) == 0 {
		t.Fatal("rdsmysql has no bundled actions")
	}
	action := actions[0]
	t.Logf("bundled action used for routing: %s", action)
	for _, service := range []string{"rdsmysql", "rds_mysql"} {
		t.Run(service, func(t *testing.T) {
			dir := reviewSafetyEnvironment(t)
			if err := safety.SavePolicy(dir, &safety.Policy{Enabled: true, Rules: []safety.Rule{{Pattern: service + ":*", Action: safety.ActionDeny}}}); err != nil {
				t.Fatal(err)
			}
			withIsolatedInvocationFlags(t)
			previousHook := executeInvocationHook
			calls, seenService := 0, ""
			executeInvocationHook = func(_ *Context, p invocationParams, _ func() (invocationInput, error)) error {
				calls++
				seenService = p.serviceName
				return nil
			}
			t.Cleanup(func() { executeInvocationHook = previousHook })
			err := reviewRunAPICommand(t, service, action)
			if err == nil || !strings.Contains(err.Error(), "blocked by safety policy") || calls != 0 {
				t.Fatalf("entered service=%q: err=%v, execution calls=%d, service checked=%q", service, err, calls, seenService)
			}
		})
	}
}

func TestSafetyApprovalFlagMatrix(t *testing.T) {
	dir := reviewSafetyEnvironment(t)
	for _, action := range []safety.Action{safety.ActionConfirm, safety.ActionDeny} {
		if err := safety.SavePolicy(dir, &safety.Policy{Enabled: true, Rules: []safety.Rule{{Pattern: "sts:*", Action: action}}}); err != nil {
			t.Fatal(err)
		}
		for _, args := range [][]string{{"--yes"}, {"-y"}, {"--region", "cn-beijing", "-y"}, {"-y", "--region", "cn-beijing"}, {"--region", "cn-beijing", "--yes"}} {
			withIsolatedInvocationFlags(t)
			previousHook := executeInvocationHook
			calls := 0
			executeInvocationHook = func(_ *Context, _ invocationParams, _ func() (invocationInput, error)) error { calls++; return nil }
			err := reviewRunAPICommand(t, append([]string{"sts", "GetCallerIdentity"}, args...)...)
			executeInvocationHook = previousHook
			if action == safety.ActionConfirm && (err != nil || calls != 1) {
				t.Errorf("confirm args=%v: err=%v, execution calls=%d", args, err, calls)
			}
			if action == safety.ActionDeny && (err == nil || calls != 0) {
				t.Errorf("deny args=%v: err=%v, execution calls=%d", args, err, calls)
			}
		}
	}
}

func TestSafetyRuleUpdateAndRemoval(t *testing.T) {
	dir := reviewSafetyEnvironment(t)
	for _, args := range [][]string{
		{"add", "--pattern", "sts:*", "--action", "confirm"},
		{"enable"},
		{"add", "--pattern", "sts:*", "--action", "deny"},
	} {
		if _, err := runSafetyPolicyCommandForTest(t, args...); err != nil {
			t.Fatal(err)
		}
	}
	policy, err := safety.LoadEffectivePolicy(dir)
	if err != nil {
		t.Fatal(err)
	}
	command := safety.CommandInfo{Product: "STS", Action: "GetCallerIdentity"}
	if len(policy.Rules) != 1 || policy.Check(command).Action != safety.ActionDeny {
		t.Fatalf("update did not replace the effective rule: %#v", policy)
	}
	if _, err := runSafetyPolicyCommandForTest(t, "remove", "--pattern", "sts:*"); err != nil {
		t.Fatal(err)
	}
	policy, err = safety.LoadEffectivePolicy(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(policy.Rules) != 0 || policy.Check(command).Action != safety.ActionAllow {
		t.Fatalf("removal did not restore unmatched allow: %#v", policy)
	}
}

func TestSafetyServiceSpellingAcrossInvocationRoutes(t *testing.T) {
	dir := reviewSafetyEnvironment(t)
	withIsolatedInvocationFlags(t)
	previousHook := executeInvocationHook
	t.Cleanup(func() {
		executeInvocationHook = previousHook
	})
	for _, args := range [][]string{
		{"rds_mysql", "AssociateAllowList"},
		{"rds_mysql", "AssociateAllowList", "--force"},
		{"rds_mysql", "ReviewUnlistedAction", "--force"},
		{"rdsmysql", "AssociateAllowList"},
		{"rdsmysql", "AssociateAllowList", "--force"},
		{"rdsmysql", "ReviewUnlistedAction", "--force"},
	} {
		for _, ruleService := range []string{"rds_mysql", "rdsmysql"} {
			if err := safety.SavePolicy(dir, &safety.Policy{Enabled: true, Rules: []safety.Rule{{Pattern: ruleService + ":*", Action: safety.ActionDeny}}}); err != nil {
				t.Fatal(err)
			}
			calls := 0
			executeInvocationHook = func(_ *Context, p invocationParams, _ func() (invocationInput, error)) error {
				calls++
				if p.serviceName != "rdsmysql" {
					t.Errorf("SDK service = %q, want canonical rdsmysql", p.serviceName)
				}
				return nil
			}
			err := reviewRunAPICommand(t, args...)
			if ruleService == args[0] {
				if err == nil || !strings.Contains(err.Error(), "blocked by safety policy") || calls != 0 {
					t.Errorf("rule=%s args=%v: error=%v calls=%d; want denial", ruleService, args, err, calls)
				}
			} else if err != nil || calls != 1 {
				t.Errorf("rule=%s args=%v: error=%v calls=%d; aliases must not expand", ruleService, args, err, calls)
			}
		}
	}
}
