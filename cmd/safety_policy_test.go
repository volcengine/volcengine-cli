package cmd

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/volcengine/volcengine-cli/internal/safety"
)

func runSafetyPolicyCommandForTest(t *testing.T, args ...string) (string, error) {
	t.Helper()
	command := newConfigureSafetyPolicyCommand()
	var output bytes.Buffer
	command.SetOut(&output)
	command.SetErr(&output)
	command.SetArgs(args)
	err := command.Execute()
	return output.String(), err
}

func TestConfigureSafetyPolicyLifecycle(t *testing.T) {
	dir, cleanup := withTestConfigDir(t)
	defer cleanup()

	if _, err := runSafetyPolicyCommandForTest(t, "add", "--pattern", "*:Delete*", "--action", "deny"); err != nil {
		t.Fatalf("add rule: %v", err)
	}
	if _, err := runSafetyPolicyCommandForTest(t, "add", "--pattern", "*:Update*", "--action", "confirm"); err != nil {
		t.Fatalf("add confirm rule: %v", err)
	}
	if _, err := runSafetyPolicyCommandForTest(t, "enable"); err != nil {
		t.Fatalf("enable policy: %v", err)
	}

	policy, err := safety.LoadPolicy(dir)
	if err != nil {
		t.Fatalf("load policy: %v", err)
	}
	if !policy.Enabled || len(policy.Rules) != 2 {
		t.Fatalf("policy = %#v, want enabled with two rules", policy)
	}

	output, err := runSafetyPolicyCommandForTest(t, "list")
	if err != nil {
		t.Fatalf("list policy: %v", err)
	}
	for _, want := range []string{"Safety policy: enabled", "*:Delete* -> deny", "*:Update* -> confirm"} {
		if !strings.Contains(output, want) {
			t.Fatalf("list output = %q, want %q", output, want)
		}
	}

	if _, err := runSafetyPolicyCommandForTest(t, "remove", "--pattern", "*:Update*"); err != nil {
		t.Fatalf("remove rule: %v", err)
	}
	if _, err := runSafetyPolicyCommandForTest(t, "disable"); err != nil {
		t.Fatalf("disable policy: %v", err)
	}
	policy, err = safety.LoadPolicy(dir)
	if err != nil {
		t.Fatalf("reload policy: %v", err)
	}
	if policy.Enabled || len(policy.Rules) != 1 || policy.Rules[0].Pattern != "*:Delete*" {
		t.Fatalf("final policy = %#v", policy)
	}
}

func TestConfigureSafetyPolicyShowUsesEffectiveEnvironment(t *testing.T) {
	_, cleanup := withTestConfigDir(t)
	defer cleanup()
	t.Setenv(safety.EnvPolicyEnabled, "true")
	t.Setenv(safety.EnvPolicyRules, "ecs:Describe*=allow,*:Delete*=deny")

	output, err := runSafetyPolicyCommandForTest(t, "show")
	if err != nil {
		t.Fatalf("show policy: %v", err)
	}
	for _, want := range []string{`"enabled": true`, `"pattern": "ecs:Describe*"`, `"action": "deny"`} {
		if !strings.Contains(output, want) {
			t.Fatalf("show output = %q, want %q", output, want)
		}
	}
}

func TestConfigureSafetyPolicyShowDefaultUsesEmptyRulesArray(t *testing.T) {
	_, cleanup := withTestConfigDir(t)
	defer cleanup()

	output, err := runSafetyPolicyCommandForTest(t, "show")
	if err != nil {
		t.Fatalf("show default policy: %v", err)
	}
	if !strings.Contains(output, `"rules": []`) {
		t.Fatalf("show output = %q, want empty rules array", output)
	}
}

func TestConfigureSafetyPolicyRejectsAllowFromAdd(t *testing.T) {
	_, cleanup := withTestConfigDir(t)
	defer cleanup()

	_, err := runSafetyPolicyCommandForTest(t, "add", "--pattern", "ecs:Describe*", "--action", "allow")
	if err == nil || !strings.Contains(err.Error(), "deny, confirm, or forbid") {
		t.Fatalf("add allow error = %v", err)
	}
}

func TestConfigureSafetyPolicyAddUpdatesExistingPattern(t *testing.T) {
	dir, cleanup := withTestConfigDir(t)
	defer cleanup()

	if _, err := runSafetyPolicyCommandForTest(t, "add", "--pattern", "ecs:Update*", "--action", "deny"); err != nil {
		t.Fatalf("add deny rule: %v", err)
	}
	if _, err := runSafetyPolicyCommandForTest(t, "add", "--pattern", "ecs:Update*", "--action", "confirm"); err != nil {
		t.Fatalf("update rule: %v", err)
	}
	policy, err := safety.LoadPolicy(dir)
	if err != nil {
		t.Fatalf("load policy: %v", err)
	}
	if len(policy.Rules) != 1 || policy.Rules[0].Action != safety.ActionConfirm {
		t.Fatalf("updated rules = %#v", policy.Rules)
	}
}

func TestExecuteInvocationEnforcesSafetyBeforeHook(t *testing.T) {
	dir, cleanup := withTestConfigDir(t)
	defer cleanup()
	if err := safety.SavePolicy(dir, &safety.Policy{
		Enabled: true,
		Rules:   []safety.Rule{{Pattern: "ecs:Delete*", Action: safety.ActionDeny}},
	}); err != nil {
		t.Fatalf("save policy: %v", err)
	}

	previousInput, previousOutput := safetyPolicyInput, safetyPolicyOutput
	safetyPolicyInput = strings.NewReader("")
	safetyPolicyOutput = &bytes.Buffer{}
	t.Cleanup(func() {
		safetyPolicyInput = previousInput
		safetyPolicyOutput = previousOutput
	})

	called := 0
	previousHook := executeInvocationHook
	executeInvocationHook = func(*Context, invocationParams, func() (invocationInput, error)) error {
		called++
		return nil
	}
	t.Cleanup(func() { executeInvocationHook = previousHook })

	err := executeInvocation(NewContext(), invocationParams{serviceName: "ecs", action: "DeleteInstance"}, func() (invocationInput, error) {
		return invocationInput{}, errors.New("input builder must not run")
	})
	if err == nil || !strings.Contains(err.Error(), "blocked by safety policy") {
		t.Fatalf("executeInvocation error = %v", err)
	}
	if called != 0 {
		t.Fatalf("execute hook called %d times for denied operation", called)
	}
}

func TestExecuteInvocationFailsClosedOnMalformedPolicy(t *testing.T) {
	dir, cleanup := withTestConfigDir(t)
	defer cleanup()
	if err := os.WriteFile(filepath.Join(dir, safety.PolicyFileName), []byte(`{"enabled":true,"rules":[{"pattern":"*:*","action":"dney"}]}`), 0o600); err != nil {
		t.Fatalf("write malformed policy: %v", err)
	}

	called := 0
	previousHook := executeInvocationHook
	executeInvocationHook = func(*Context, invocationParams, func() (invocationInput, error)) error {
		called++
		return nil
	}
	t.Cleanup(func() { executeInvocationHook = previousHook })

	err := executeInvocation(NewContext(), invocationParams{serviceName: "sts", action: "GetCallerIdentity"}, func() (invocationInput, error) {
		return invocationInput{}, nil
	})
	if err == nil || !strings.Contains(err.Error(), "load effective safety policy") {
		t.Fatalf("malformed policy error = %v", err)
	}
	if called != 0 {
		t.Fatalf("execution hook called %d times with malformed policy", called)
	}
}

func TestSafetyPolicyDenialIsLocalized(t *testing.T) {
	dir, cleanup := withTestConfigDir(t)
	defer cleanup()
	if err := safety.SavePolicy(dir, &safety.Policy{
		Enabled: true,
		Rules:   []safety.Rule{{Pattern: "ecs:Delete*", Action: safety.ActionDeny}},
	}); err != nil {
		t.Fatalf("save policy: %v", err)
	}
	restoreLanguage := setLanguageForTest(LanguageSimplifiedChinese)
	defer restoreLanguage()

	err := executeInvocation(NewContext(), invocationParams{serviceName: "ecs", action: "DeleteInstance"}, func() (invocationInput, error) {
		return invocationInput{}, nil
	})
	if err == nil || !strings.Contains(err.Error(), "安全策略") {
		t.Fatalf("localized denial error = %v", err)
	}
}

func TestExecuteInvocationRequiresThenAcceptsExplicitApproval(t *testing.T) {
	dir, cleanup := withTestConfigDir(t)
	defer cleanup()
	if err := safety.SavePolicy(dir, &safety.Policy{
		Enabled: true,
		Rules:   []safety.Rule{{Pattern: "ecs:Update*", Action: safety.ActionConfirm}},
	}); err != nil {
		t.Fatalf("save policy: %v", err)
	}

	previousInput, previousOutput := safetyPolicyInput, safetyPolicyOutput
	safetyPolicyInput = strings.NewReader("")
	safetyPolicyOutput = &bytes.Buffer{}
	t.Cleanup(func() {
		safetyPolicyInput = previousInput
		safetyPolicyOutput = previousOutput
	})

	called := 0
	previousHook := executeInvocationHook
	executeInvocationHook = func(*Context, invocationParams, func() (invocationInput, error)) error {
		called++
		return nil
	}
	t.Cleanup(func() { executeInvocationHook = previousHook })

	callContext := NewContext()
	params := invocationParams{serviceName: "ecs", action: "UpdateInstance"}
	err := executeInvocation(callContext, params, func() (invocationInput, error) { return invocationInput{}, nil })
	if err == nil || !strings.Contains(err.Error(), "--yes") {
		t.Fatalf("unapproved invocation error = %v", err)
	}
	if called != 0 {
		t.Fatalf("hook called before approval: %d", called)
	}

	yesFlag, err := callContext.fixedFlags.AddByName("yes")
	if err != nil {
		t.Fatalf("add yes flag: %v", err)
	}
	yesFlag.SetValue("true")
	if err := executeInvocation(callContext, params, func() (invocationInput, error) { return invocationInput{}, nil }); err != nil {
		t.Fatalf("approved invocation error = %v", err)
	}
	if called != 1 {
		t.Fatalf("approved hook calls = %d, want 1", called)
	}
}

func TestForceInvocationUsesSafetyPolicy(t *testing.T) {
	dir, cleanup := withTestConfigDir(t)
	defer cleanup()
	if err := safety.SavePolicy(dir, &safety.Policy{
		Enabled: true,
		Rules:   []safety.Rule{{Pattern: "sts:GetCallerIdentity", Action: safety.ActionDeny}},
	}); err != nil {
		t.Fatalf("save policy: %v", err)
	}

	called := 0
	previousHook := executeInvocationHook
	executeInvocationHook = func(*Context, invocationParams, func() (invocationInput, error)) error {
		called++
		return nil
	}
	t.Cleanup(func() { executeInvocationHook = previousHook })

	callContext := NewContext()
	forceFlag, err := callContext.fixedFlags.AddByName("force")
	if err != nil {
		t.Fatalf("add force flag: %v", err)
	}
	forceFlag.SetValue("true")
	err = doForceAction(callContext, "sts", "GetCallerIdentity")
	if err == nil || !strings.Contains(err.Error(), "blocked by safety policy") {
		t.Fatalf("force invocation error = %v", err)
	}
	if called != 0 {
		t.Fatalf("force invocation reached execution hook %d times", called)
	}
}

func TestSafetyApprovalSystemFlags(t *testing.T) {
	for _, args := range [][]string{{"--yes"}, {"-y"}} {
		callContext := NewContext()
		if _, err := NewParser(args).ReadArgs(callContext); err != nil {
			t.Fatalf("parse %v: %v", args, err)
		}
		if !safetyConfirmationSkipped(callContext) {
			t.Fatalf("safetyConfirmationSkipped() = false for %v", args)
		}
	}
}

func TestShortSafetyApprovalFlagMustFollowAction(t *testing.T) {
	_, err := resolveSystemFlags([]string{"sts", "-y", "GetCallerIdentity"})
	if err == nil || !strings.Contains(err.Error(), "must be specified after action") {
		t.Fatalf("resolveSystemFlags() error = %v, want position error", err)
	}
}
