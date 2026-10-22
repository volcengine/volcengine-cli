package safety

import (
	"strings"
	"testing"
)

func checkPreset(t *testing.T, name string, want map[string]Action) {
	t.Helper()
	rules, ok := PresetRules(name)
	if !ok || len(rules) == 0 {
		t.Fatalf("preset %s is missing", name)
	}
	policy := &Policy{Enabled: true, Rules: rules}
	if err := validatePolicy(policy); err != nil {
		t.Fatalf("preset %s is not a valid policy: %v", name, err)
	}
	for command, action := range want {
		service, api, _ := strings.Cut(command, ":")
		if got := policy.Check(CommandInfo{Product: service, Action: api}).Action; got != action {
			t.Errorf("%s %s = %s, want %s", name, command, got, action)
		}
	}
}

func TestConfirmWritesPreset(t *testing.T) {
	rules, _ := PresetRules(PresetConfirmWrites)
	if last := rules[len(rules)-1]; last.Pattern != "*:*" || last.Action != ActionConfirm {
		t.Fatalf("last rule = %+v, want *:*=confirm", last)
	}
	checkPreset(t, PresetConfirmWrites, map[string]Action{
		"ecs:DescribeInstances":                      ActionAllow,
		"vpc:ListPrefixLists":                        ActionAllow,
		"spark:listApplication":                      ActionAllow,
		"billing:QueryBalanceAcct":                   ActionAllow,
		"id:BatchGetDepartments":                     ActionAllow,
		"bmq:TopicExist":                             ActionAllow,
		"rdsmysqlv2:DownloadSSLCertificate":          ActionAllow,
		"ecs:RunInstances":                           ActionConfirm,
		"ecs:DeleteInstance":                         ActionConfirm,
		"ecs:ExportImage":                            ActionConfirm,
		"seccenter20240508:ExportVirusAlarmListData": ActionConfirm,
		"rdsmysqlv2:DownloadBackup":                  ActionConfirm,
		"dts20180101:PreCheckAsync":                  ActionConfirm,
		"dns:RetrieveZone":                           ActionConfirm,
		"waf:QueryLLMGenerate":                       ActionConfirm,
		"kms:GetSecretValue":                         ActionConfirm,
		"id:GetRoleCredentials":                      ActionConfirm,
		"ecs:DescribeInstanceVncUrl":                 ActionConfirm,
		"vefaas:GetCodeUploadAddress":                ActionConfirm,
		"newservice:AnyAction":                       ActionConfirm,
	})
}

func TestConfirmDeletesPreset(t *testing.T) {
	checkPreset(t, PresetConfirmDeletes, map[string]Action{
		"ecs:DescribeInstances":                 ActionAllow,
		"rdsmysqlv2:DescribeDeletedDBInstances": ActionAllow,
		"cbr:DescribeRestoreJobs":               ActionAllow,
		"ecs:RunInstances":                      ActionAllow,
		"vpc:CreateVpc":                         ActionAllow,
		"ecs:DeleteInstance":                    ActionConfirm,
		"kms:ScheduleKeyDeletion":               ActionConfirm,
		"i18nopenapi:ProjectDownloadTaskDelete": ActionConfirm,
		"advdefence:DelHostRule":                ActionConfirm,
		"vpc:RemoveAclEntries":                  ActionConfirm,
		"ecs:TerminateInstances":                ActionConfirm,
		"graph:DestroyInstance":                 ActionConfirm,
		"aidap:BranchRestore":                   ActionConfirm,
		"cp:TriggerManagedAppRollback":          ActionConfirm,
		"redis:FlushDBInstance":                 ActionConfirm,
		"organization:QuitOrganization":         ActionConfirm,
	})
	if _, ok := PresetRules("unknown"); ok {
		t.Fatal("unknown preset accepted")
	}
}

// The rules travel to a hosted sandbox as VOLCENGINE_SAFETY_POLICY_RULES.
func TestPresetsSurviveEnvironmentEncoding(t *testing.T) {
	for _, name := range PresetNames() {
		rules, _ := PresetRules(name)
		entries := make([]string, 0, len(rules))
		for _, rule := range rules {
			entries = append(entries, rule.Pattern+"="+string(rule.Action))
		}
		t.Setenv(EnvPolicyEnabled, "true")
		t.Setenv(EnvPolicyRules, strings.Join(entries, ","))
		policy, err := mergePolicyFromEnvironment(DefaultPolicy())
		if err != nil || len(policy.Rules) != len(rules) {
			t.Fatalf("%s environment policy = %v, %v", name, policy, err)
		}
	}
}
