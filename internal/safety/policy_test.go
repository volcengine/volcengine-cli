package safety

import (
	"os"
	"path/filepath"
	"testing"
)

func TestPolicyCheckUsesFirstCaseInsensitiveWildcardMatch(t *testing.T) {
	policy := &Policy{
		Enabled: true,
		Rules: []Rule{
			{Pattern: "ecs:Describe*", Action: ActionAllow},
			{Pattern: "ecs:*", Action: ActionDeny},
		},
	}

	allowed := policy.Check(CommandInfo{Product: "ECS", Action: "describeInstances"})
	if !allowed.Matched || allowed.Action != ActionAllow {
		t.Fatalf("DescribeInstances result = %#v, want first allow rule", allowed)
	}

	denied := policy.Check(CommandInfo{Product: "ecs", Action: "DeleteInstance"})
	if !denied.Matched || denied.Action != ActionDeny {
		t.Fatalf("DeleteInstance result = %#v, want deny rule", denied)
	}
}

func TestPolicyCheckDoesNotExpandCommandSpellings(t *testing.T) {
	policy := &Policy{
		Enabled: true,
		Rules:   []Rule{{Pattern: "ecs:DeleteInstance", Action: ActionDeny}},
	}

	result := policy.Check(CommandInfo{Product: "ecs", Action: "delete-instance"})
	if result.Matched || result.Action != ActionAllow {
		t.Fatalf("kebab-case alias result = %#v, want unmatched allow", result)
	}
}

func TestPolicyCheckDefaultsToAllow(t *testing.T) {
	for _, policy := range []*Policy{
		nil,
		DefaultPolicy(),
		{Enabled: true, Rules: []Rule{{Pattern: "ecs:Delete*", Action: ActionDeny}}},
	} {
		result := policy.Check(CommandInfo{Product: "ecs", Action: "DescribeInstances"})
		if result.Action != ActionAllow {
			t.Fatalf("policy %#v result = %#v, want allow", policy, result)
		}
	}
}

func TestForbidIsConfirmationAlias(t *testing.T) {
	policy := &Policy{
		Enabled: true,
		Rules:   []Rule{{Pattern: "ecs:Update*", Action: ActionForbid}},
	}
	result := policy.Check(CommandInfo{Product: "ecs", Action: "UpdateInstance"})
	if !result.Matched || result.Action != ActionConfirm {
		t.Fatalf("forbid result = %#v, want confirmation", result)
	}
}

func TestSaveAndLoadPolicy(t *testing.T) {
	dir := t.TempDir()
	want := &Policy{
		Enabled: true,
		Rules: []Rule{
			{Pattern: "ecs:Describe*", Action: ActionAllow},
			{Pattern: "*:Delete*", Action: ActionDeny},
			{Pattern: "*:Update*", Action: ActionConfirm},
		},
	}
	if err := SavePolicy(dir, want); err != nil {
		t.Fatalf("SavePolicy() error = %v", err)
	}

	path := filepath.Join(dir, PolicyFileName)
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat policy: %v", err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Fatalf("policy mode = %#o, want 0600", got)
	}

	got, err := LoadPolicy(dir)
	if err != nil {
		t.Fatalf("LoadPolicy() error = %v", err)
	}
	if !got.Enabled || len(got.Rules) != len(want.Rules) {
		t.Fatalf("loaded policy = %#v, want %#v", got, want)
	}
	for i := range want.Rules {
		if got.Rules[i] != want.Rules[i] {
			t.Fatalf("loaded rule[%d] = %#v, want %#v", i, got.Rules[i], want.Rules[i])
		}
	}
}

func TestLoadPolicyMissingReturnsDisabledDefault(t *testing.T) {
	policy, err := LoadPolicy(t.TempDir())
	if err != nil {
		t.Fatalf("LoadPolicy() error = %v", err)
	}
	if policy.Enabled || len(policy.Rules) != 0 {
		t.Fatalf("default policy = %#v, want disabled and empty", policy)
	}
	if policy.Rules == nil {
		t.Fatal("default policy rules = nil, want an empty JSON array")
	}
}

func TestLoadEffectivePolicyPreservesEmptyRulesArray(t *testing.T) {
	policy, err := LoadEffectivePolicy(t.TempDir())
	if err != nil {
		t.Fatalf("LoadEffectivePolicy() error = %v", err)
	}
	if policy.Rules == nil {
		t.Fatal("effective policy rules = nil, want an empty JSON array")
	}
}

func TestLoadPolicyRejectsMalformedOrUnknownConfiguration(t *testing.T) {
	for _, content := range []string{
		`null`,
		`[]`,
		`{"enabled":true,"rules":[{"pattern":"*:Delete*","action":"unknown"}]}`,
		`{"enabled":true,"rules":[],"future":true}`,
		`{"enabled":true,"rules":[]} trailing`,
	} {
		dir := t.TempDir()
		if err := os.WriteFile(filepath.Join(dir, PolicyFileName), []byte(content), 0o600); err != nil {
			t.Fatalf("write malformed policy: %v", err)
		}
		if _, err := LoadPolicy(dir); err == nil {
			t.Fatalf("LoadPolicy(%q) unexpectedly succeeded", content)
		}
	}
}

func TestLoadEffectivePolicyEnvironmentOverridesFile(t *testing.T) {
	dir := t.TempDir()
	if err := SavePolicy(dir, &Policy{
		Enabled: true,
		Rules:   []Rule{{Pattern: "ecs:*", Action: ActionDeny}},
	}); err != nil {
		t.Fatalf("SavePolicy() error = %v", err)
	}

	t.Setenv(EnvPolicyEnabled, "false")
	t.Setenv(EnvPolicyRules, "ecs:Describe*=allow,*:Delete*=confirm")
	policy, err := LoadEffectivePolicy(dir)
	if err != nil {
		t.Fatalf("LoadEffectivePolicy() error = %v", err)
	}
	if policy.Enabled {
		t.Fatal("environment enabled=false did not override file")
	}
	if len(policy.Rules) != 2 || policy.Rules[0].Action != ActionAllow || policy.Rules[1].Action != ActionConfirm {
		t.Fatalf("effective rules = %#v", policy.Rules)
	}
}

func TestLoadEffectivePolicyEmptyEnvironmentRulesClearsFileRules(t *testing.T) {
	dir := t.TempDir()
	if err := SavePolicy(dir, &Policy{
		Enabled: true,
		Rules:   []Rule{{Pattern: "ecs:*", Action: ActionDeny}},
	}); err != nil {
		t.Fatalf("SavePolicy() error = %v", err)
	}
	t.Setenv(EnvPolicyRules, "")
	policy, err := LoadEffectivePolicy(dir)
	if err != nil {
		t.Fatalf("LoadEffectivePolicy() error = %v", err)
	}
	if policy.Rules == nil || len(policy.Rules) != 0 {
		t.Fatalf("effective rules = %#v, want non-nil empty slice", policy.Rules)
	}
}

func TestLoadEffectivePolicyRejectsInvalidEnvironment(t *testing.T) {
	t.Run("enabled", func(t *testing.T) {
		t.Setenv(EnvPolicyEnabled, "sometimes")
		if _, err := LoadEffectivePolicy(t.TempDir()); err == nil {
			t.Fatal("invalid enabled override unexpectedly succeeded")
		}
	})

	t.Run("rules", func(t *testing.T) {
		t.Setenv(EnvPolicyRules, "*:Delete*=dney")
		if _, err := LoadEffectivePolicy(t.TempDir()); err == nil {
			t.Fatal("invalid rules override unexpectedly succeeded")
		}
	})
}
