package cmd

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
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

func TestSafetyServiceAliases(t *testing.T) {
	for service, want := range map[string]string{
		"rdsmysql":           "rds_mysql,rds_mysql_v2,rdsmysqlv2",
		"rds_mysql":          "rds_mysql_v2,rdsmysql,rdsmysqlv2",
		"rdsmysqlv2":         "rds_mysql,rds_mysql_v2,rdsmysql",
		"RDS_MYSQL_V2":       "rds_mysql,rdsmysql,rdsmysqlv2",
		"ml_platform":        "mlplatform,mlplatform20240701",
		"cloudtrail20180101": "cloud_trail,cloudtrail",
		"cloud_trail":        "cloudtrail,cloudtrail20180101",
		"vod":                "vod20250101,vod20260101",
		"ecs":                "",
		"foo_bar":            "",
		"foo20250101":        "",
	} {
		if got := strings.Join(safetyServiceAliases(service), ","); got != want {
			t.Errorf("safetyServiceAliases(%q) = %q, want %q", service, got, want)
		}
	}
}

func TestSafetyServiceSpellingsBelongToOneService(t *testing.T) {
	for command, sdkService := range serviceMapping {
		if owner, ok := serviceMapping[sdkService]; ok && owner != sdkService {
			t.Errorf("SDK service %q of command %q is also the command of SDK service %q", sdkService, command, owner)
		}
	}
	for _, name := range compatible_support_cmd {
		if _, ok := serviceMapping[strings.ReplaceAll(name, "_", "")]; !ok {
			t.Errorf("compatible command %q has no canonical service", name)
		}
	}
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

func TestConfigureSafetyPolicyAddRejectsUnsupportedActions(t *testing.T) {
	_, cleanup := withTestConfigDir(t)
	defer cleanup()

	for _, action := range []string{"allow"} {
		_, err := runSafetyPolicyCommandForTest(t, "add", "--pattern", "ecs:Describe*", "--action", action)
		if err == nil || !strings.Contains(err.Error(), "deny or confirm") {
			t.Errorf("add %q error = %v", action, err)
		}
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
	dir := reviewSafetyEnvironment(t)
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
	if err := executeInvocation(NewContext(), params, func() (invocationInput, error) { return invocationInput{}, nil }); err == nil {
		t.Fatal("next invocation without yes must require confirmation again")
	}
	if called != 1 {
		t.Fatalf("approval persisted into next invocation: hook calls = %d", called)
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

func TestSafetyConfirmationNoticeRequiresFreshApprovalInBothLanguages(t *testing.T) {
	dir := reviewSafetyEnvironment(t)
	if err := safety.SavePolicy(dir, &safety.Policy{Enabled: true, Rules: []safety.Rule{{Pattern: "sts:*", Action: safety.ActionConfirm}}}); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		language Language
		want     []string
	}{
		// English semantics are covered in internal/safety; template equality is
		// checked below. Keep the translated behavior independently asserted.
		{LanguageEnglish, []string{"current environment is non-interactive."}},
		{LanguageSimplifiedChinese, []string{"停止", "简要说明", "结束当前轮次并等待", "用户的新回复", "仅在确认后", "内部", "仅重试一次", "无需向用户解释此参数", "即使失败", "每次新操作先不带 --yes/yes=true", "仅遇拦截才解释并询问", "不得复用历史批准"}},
	} {
		t.Run(string(tc.language), func(t *testing.T) {
			restore := setLanguageForTest(tc.language)
			defer restore()
			err := enforceSafetyPolicy("sts", "GetCallerIdentity", false, false, tr("call the same MCP tool again with yes=true"))
			if err == nil {
				t.Fatal("unapproved invocation must be blocked")
			}
			for _, want := range append(tc.want, "yes=true") {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("notice missing %q: %s", want, err)
				}
			}
		})
	}
}

func TestSafetyConfirmationTemplateMatchesDefault(t *testing.T) {
	dir := reviewSafetyEnvironment(t)
	if err := safety.SavePolicy(dir, &safety.Policy{Enabled: true, Rules: []safety.Rule{{Pattern: "sts:*", Action: safety.ActionConfirm}}}); err != nil {
		t.Fatal(err)
	}
	restore := setLanguageForTest(LanguageEnglish)
	defer restore()
	err := enforceSafetyPolicy("sts", "GetCallerIdentity", false, false, "retry")
	want := fmt.Sprintf(safety.ConfirmationNeededMessage, "sts", "GetCallerIdentity", "retry")
	if err == nil || err.Error() != want {
		t.Fatalf("localized source template differs from safety default: %v", err)
	}
	if _, ok := combinedTranslationCatalog(t)[safety.ConfirmationNeededMessage]; !ok {
		t.Fatal("shared confirmation template has no Chinese translation")
	}
}

// Every explicit (non-wildcard service) rule of the official presets must
// name a bundled API, so metadata updates cannot silently orphan a rule.
func TestSafetyPolicyPresetsNameBundledAPIs(t *testing.T) {
	var rules []safety.Rule
	for _, name := range safety.PresetNames() {
		preset, _ := safety.PresetRules(name)
		rules = append(rules, preset...)
	}
	for _, rule := range rules {
		service, action, _ := strings.Cut(rule.Pattern, ":")
		if service == "*" {
			continue
		}
		matched := false
		for _, candidate := range rootSupport.GetAllAction(service) {
			if strings.HasSuffix(action, "*") && strings.HasPrefix(candidate, strings.TrimSuffix(action, "*")) || candidate == action {
				matched = true
				break
			}
		}
		if !matched {
			t.Errorf("preset rule %s names no bundled API", rule.Pattern)
		}
	}
}

func TestConfigureSafetyPolicyPreset(t *testing.T) {
	dir, cleanup := withTestConfigDir(t)
	defer cleanup()
	previousOutput, previousInput := safetyPolicyOutput, safetyPolicyInput
	output := &bytes.Buffer{}
	safetyPolicyOutput, safetyPolicyInput = output, strings.NewReader("")
	defer func() { safetyPolicyOutput, safetyPolicyInput = previousOutput, previousInput }()
	preset, _ := safety.PresetRules(safety.PresetConfirmWrites)

	if _, err := runSafetyPolicyCommandForTest(t, "preset", "unknown"); err == nil || !strings.Contains(err.Error(), "confirm-writes") {
		t.Fatalf("unknown preset error = %v", err)
	}
	// No policy yet: the preset is written and enabled.
	if _, err := runSafetyPolicyCommandForTest(t, "preset", "confirm-writes"); err != nil {
		t.Fatal(err)
	}
	policy, err := safety.LoadPolicy(dir)
	if err != nil || !policy.Enabled || len(policy.Rules) != len(preset) {
		t.Fatalf("policy = %+v, %v", policy, err)
	}

	// Existing rules without a choice: a non-interactive run must not guess.
	if _, err := runSafetyPolicyCommandForTest(t, "add", "--pattern", "ecs:DescribeInstances", "--action", "deny"); err != nil {
		t.Fatal(err)
	}
	if _, err := runSafetyPolicyCommandForTest(t, "preset", "confirm-writes"); err == nil || !strings.Contains(err.Error(), "--overwrite") {
		t.Fatalf("preset over existing rules without a choice: %v", err)
	}
	if _, err := runSafetyPolicyCommandForTest(t, "preset", "confirm-writes", "--overwrite", "--append"); err == nil {
		t.Fatal("--overwrite with --append accepted")
	}

	// Append keeps existing rules first and skips duplicate patterns.
	if err := safety.SavePolicy(dir, &safety.Policy{Rules: []safety.Rule{{Pattern: "ecs:DescribeInstances", Action: safety.ActionDeny}, {Pattern: "*:*", Action: safety.ActionAllow}}}); err != nil {
		t.Fatal(err)
	}
	output.Reset()
	if _, err := runSafetyPolicyCommandForTest(t, "preset", "confirm-writes", "--append"); err != nil {
		t.Fatal(err)
	}
	policy, _ = safety.LoadPolicy(dir)
	if !policy.Enabled || len(policy.Rules) != len(preset)+1 || policy.Rules[0].Pattern != "ecs:DescribeInstances" || policy.Rules[1].Pattern != "*:*" || policy.Rules[1].Action != safety.ActionAllow {
		t.Fatalf("appended policy = %d rules, first %+v %+v", len(policy.Rules), policy.Rules[0], policy.Rules[1])
	}
	if !strings.Contains(output.String(), "matches every operation") {
		t.Fatalf("append did not warn about the catch-all rule: %q", output.String())
	}

	// Overwrite replaces the rules.
	if _, err := runSafetyPolicyCommandForTest(t, "preset", "confirm-writes", "--overwrite"); err != nil {
		t.Fatal(err)
	}
	policy, _ = safety.LoadPolicy(dir)
	if len(policy.Rules) != len(preset) || policy.Rules[0] != preset[0] {
		t.Fatalf("overwritten policy = %d rules", len(policy.Rules))
	}
}

// add puts new rules first, so they apply even after a preset's catch-all.
func TestConfigureSafetyPolicyAddAfterPreset(t *testing.T) {
	dir, cleanup := withTestConfigDir(t)
	defer cleanup()
	previousOutput, previousInput := safetyPolicyOutput, safetyPolicyInput
	safetyPolicyOutput, safetyPolicyInput = &bytes.Buffer{}, strings.NewReader("")
	defer func() { safetyPolicyOutput, safetyPolicyInput = previousOutput, previousInput }()
	if _, err := runSafetyPolicyCommandForTest(t, "preset", "confirm-writes"); err != nil {
		t.Fatal(err)
	}
	if _, err := runSafetyPolicyCommandForTest(t, "add", "--pattern", "ecs:Delete*", "--action", "deny"); err != nil {
		t.Fatal(err)
	}
	policy, err := safety.LoadPolicy(dir)
	if err != nil {
		t.Fatal(err)
	}
	if policy.Rules[0].Pattern != "ecs:Delete*" || policy.Rules[0].Action != safety.ActionDeny {
		t.Fatalf("first rule = %+v", policy.Rules[0])
	}
	if got := policy.Check(safety.CommandInfo{Product: "ecs", Action: "DeleteInstance"}).Action; got != safety.ActionDeny {
		t.Fatalf("ecs:DeleteInstance = %s, want deny", got)
	}
}

// add keeps the policy in the order deny rules, then allow and confirm
// rules, then a *:* catch-all, so a new confirm rule never overrides a deny
// and is never hidden by an allow rule; where no place satisfies both, it
// refuses and leaves the policy unchanged.
func TestSafetyPolicyAddKeepsDenyConfirmCatchAllOrder(t *testing.T) {
	type env struct {
		dir   string
		check func(string) safety.Action
		rules func() []safety.Rule
		run   func(...string) (string, error)
	}
	setup := func(t *testing.T) env {
		dir, cleanup := withTestConfigDir(t)
		t.Cleanup(cleanup)
		load := func() *safety.Policy {
			t.Helper()
			policy, err := safety.LoadPolicy(dir)
			if err != nil {
				t.Fatal(err)
			}
			return policy
		}
		return env{
			dir: dir,
			check: func(api string) safety.Action {
				t.Helper()
				service, action, _ := strings.Cut(api, ":")
				return load().Check(safety.CommandInfo{Product: service, ProductAliases: safetyServiceAliases(service), Action: action}).Action
			},
			rules: func() []safety.Rule { return load().Rules },
			run: func(args ...string) (string, error) {
				t.Helper()
				return runSafetyPolicyCommandForTest(t, args...)
			},
		}
	}
	mustRun := func(t *testing.T, e env, args ...string) string {
		t.Helper()
		output, err := e.run(args...)
		if err != nil {
			t.Fatalf("%v: %v", args, err)
		}
		return output
	}
	want := func(t *testing.T, e env, api string, action safety.Action) {
		t.Helper()
		if got := e.check(api); got != action {
			t.Fatalf("%s = %s, want %s", api, got, action)
		}
	}

	t.Run("deny first, confirm after the deny rules", func(t *testing.T) {
		e := setup(t)
		mustRun(t, e, "add", "--pattern", "ecs:DeleteInstance", "--action", "deny")
		mustRun(t, e, "enable")
		mustRun(t, e, "add", "--pattern", "ecs:*", "--action", "confirm")
		want(t, e, "ecs:DeleteInstance", safety.ActionDeny)
		want(t, e, "ecs:RunInstances", safety.ActionConfirm)
	})
	t.Run("preset rule updated to deny, then confirm", func(t *testing.T) {
		e := setup(t)
		mustRun(t, e, "preset", "confirm-deletes", "--overwrite")
		mustRun(t, e, "add", "--pattern", "*:BatchReset*", "--action", "deny")
		mustRun(t, e, "add", "--pattern", "ecs:DescribeInstances", "--action", "confirm")
		want(t, e, "ecs:DescribeInstances", safety.ActionConfirm)
		want(t, e, "ecs:BatchResetInstances", safety.ActionDeny)
	})
	t.Run("deny updated behind a broader confirm", func(t *testing.T) {
		e := setup(t)
		mustRun(t, e, "add", "--pattern", "ecs:StopInstance", "--action", "deny")
		mustRun(t, e, "enable")
		mustRun(t, e, "add", "--pattern", "ecs:DeleteInstance", "--action", "confirm")
		mustRun(t, e, "add", "--pattern", "ecs:*", "--action", "confirm")
		mustRun(t, e, "add", "--pattern", "ecs:DeleteInstance", "--action", "deny")
		want(t, e, "ecs:DeleteInstance", safety.ActionDeny)
	})
	t.Run("deny kept for any spelling or character", func(t *testing.T) {
		e := setup(t)
		mustRun(t, e, "add", "--pattern", "cloud_*:DeleteTrail", "--action", "deny")
		mustRun(t, e, "add", "--pattern", "ecs:De\u017fcribeInstances", "--action", "deny")
		mustRun(t, e, "enable")
		mustRun(t, e, "add", "--pattern", "cloudtrail*:*", "--action", "confirm")
		mustRun(t, e, "add", "--pattern", "ecs:Describe*", "--action", "confirm")
		want(t, e, "cloudtrail:DeleteTrail", safety.ActionDeny)
		want(t, e, "ecs:DescribeInstances", safety.ActionDeny)
	})
	t.Run("duplicate patterns merge", func(t *testing.T) {
		e := setup(t)
		if err := safety.SavePolicy(e.dir, &safety.Policy{Enabled: true, Rules: []safety.Rule{
			{Pattern: "ecs:DescribeInstances", Action: safety.ActionConfirm},
			{Pattern: "ECS:DESCRIBEINSTANCES", Action: safety.ActionConfirm},
		}}); err != nil {
			t.Fatal(err)
		}
		output := mustRun(t, e, "add", "--pattern", "ecs:describeinstances", "--action", "deny")
		want(t, e, "ecs:DescribeInstances", safety.ActionDeny)
		if rules := e.rules(); len(rules) != 1 || rules[0].Pattern != "ecs:DescribeInstances" || !strings.Contains(output, "2") {
			t.Fatalf("rules = %+v, output = %q", rules, output)
		}
	})
	t.Run("catch-all stays last", func(t *testing.T) {
		e := setup(t)
		mustRun(t, e, "preset", "confirm-writes", "--overwrite")
		mustRun(t, e, "add", "--pattern", "*:*", "--action", "deny")
		want(t, e, "ecs:DescribeInstances", safety.ActionAllow)
		want(t, e, "ecs:RunInstances", safety.ActionDeny)
		if rules := e.rules(); rules[len(rules)-1].Pattern != "*:*" {
			t.Fatalf("*:* moved: last rule %+v", rules[len(rules)-1])
		}
		output := mustRun(t, e, "add", "--pattern", "*:*", "--action", "confirm")
		want(t, e, "ecs:DescribeInstances", safety.ActionAllow)
		want(t, e, "ecs:RunInstances", safety.ActionConfirm)
		if !strings.Contains(output, "Warning") && !strings.Contains(output, "警告") {
			t.Fatalf("deny to confirm printed %q, want a warning", output)
		}
	})
	t.Run("confirm refused under a default deny", func(t *testing.T) {
		e := setup(t)
		mustRun(t, e, "preset", "deny-writes", "--overwrite")
		before := e.rules()
		if _, err := e.run("add", "--pattern", "kms:*", "--action", "confirm"); err == nil || !strings.Contains(err.Error(), "*:*") {
			t.Fatalf("add kms:*=confirm error = %v", err)
		}
		if !reflect.DeepEqual(e.rules(), before) {
			t.Fatal("a refused add changed the policy")
		}
		want(t, e, "kms:CreateKey", safety.ActionDeny)
	})
	t.Run("confirm refused when a deny follows allow rules", func(t *testing.T) {
		e := setup(t)
		mustRun(t, e, "preset", "confirm-deletes", "--overwrite")
		mustRun(t, e, "preset", "deny-writes", "--append")
		before := e.rules()
		if _, err := e.run("add", "--pattern", "ecs:DescribeInstances", "--action", "confirm"); err == nil || !strings.Contains(err.Error(), "deny") {
			t.Fatalf("add error = %v", err)
		}
		if !reflect.DeepEqual(e.rules(), before) {
			t.Fatal("a refused add changed the policy")
		}
		// A deny still goes first in that policy.
		mustRun(t, e, "add", "--pattern", "ecs:DescribeInstances", "--action", "deny")
		want(t, e, "ecs:DescribeInstances", safety.ActionDeny)
	})
	t.Run("misplaced catch-all", func(t *testing.T) {
		e := setup(t)
		mustRun(t, e, "preset", "deny-writes", "--overwrite")
		mustRun(t, e, "preset", "confirm-deletes", "--append")
		if _, err := e.run("add", "--pattern", "*:*", "--action", "confirm"); err == nil || !strings.Contains(err.Error(), "*:*") {
			t.Fatalf("add *:* error = %v", err)
		}
	})
}
