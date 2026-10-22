package safety

import (
	"os"
	"strings"
	"testing"
)

func TestReadOnlyPreset(t *testing.T) {
	rules, ok := PresetRules(PresetReadOnly)
	if !ok || len(rules) == 0 {
		t.Fatal("readonly preset is missing")
	}
	policy := &Policy{Enabled: true, Rules: rules}
	if err := validatePolicy(policy); err != nil {
		t.Fatalf("readonly preset is not a valid policy: %v", err)
	}
	if last := rules[len(rules)-1]; last.Pattern != "*:*" || last.Action != ActionConfirm {
		t.Fatalf("last rule = %+v, want *:*=confirm", last)
	}
	for command, want := range map[string]Action{
		"ecs:DescribeInstances":                      ActionAllow,
		"vpc:ListPrefixLists":                        ActionAllow,
		"spark:listApplication":                      ActionAllow,
		"billing:QueryBalanceAcct":                   ActionAllow,
		"id:BatchGetDepartments":                     ActionAllow,
		"bmq:TopicExist":                             ActionAllow,
		"seccenter20240508:ExportVirusAlarmListData": ActionAllow,
		"ecs:RunInstances":                           ActionConfirm,
		"ecs:DeleteInstance":                         ActionConfirm,
		"ecs:ExportImage":                            ActionConfirm,
		"dns:RetrieveZone":                           ActionConfirm,
		"kms:GetSecretValue":                         ActionConfirm,
		"id:GetRoleCredentials":                      ActionConfirm,
		"ecs:DescribeInstanceVncUrl":                 ActionConfirm,
		"vefaas:GetCodeUploadAddress":                ActionConfirm,
		"newservice:AnyAction":                       ActionConfirm,
	} {
		service, action, _ := strings.Cut(command, ":")
		if got := policy.Check(CommandInfo{Product: service, Action: action}).Action; got != want {
			t.Errorf("%s = %s, want %s", command, got, want)
		}
	}
	if _, ok := PresetRules("unknown"); ok {
		t.Fatal("unknown preset accepted")
	}
}

// The preset travels to the hosted sandbox as VOLCENGINE_SAFETY_POLICY_RULES.
func TestReadOnlyPresetSurvivesEnvironmentEncoding(t *testing.T) {
	rules, _ := PresetRules(PresetReadOnly)
	entries := make([]string, 0, len(rules))
	for _, rule := range rules {
		entries = append(entries, rule.Pattern+"="+string(rule.Action))
	}
	t.Setenv(EnvPolicyEnabled, "true")
	t.Setenv(EnvPolicyRules, strings.Join(entries, ","))
	policy, err := mergePolicyFromEnvironment(DefaultPolicy())
	if err != nil || len(policy.Rules) != len(rules) {
		t.Fatalf("environment policy = %d rules, %v", len(policy.Rules), err)
	}
	_ = os.Unsetenv(EnvPolicyRules)
}
