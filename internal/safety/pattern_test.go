package safety

import (
	"bytes"
	"encoding/json"
	"os"
	"strings"
	"testing"
)

func TestPolicyRejectsMalformedPatterns(t *testing.T) {
	for _, pattern := range []string{
		"GetCallerIdentity", "sts.GetCallerIdentity", "sts GetCallerIdentity", "sts",
		"", "*", ":", ":GetCallerIdentity", "sts:", "sts::GetCallerIdentity",
		"sts:Get:CallerIdentity", "sts :GetCallerIdentity", "sts: GetCallerIdentity",
		"st\ts:GetCallerIdentity", "sts:Get\nCallerIdentity", "sts:\u2003GetCallerIdentity",
	} {
		t.Run(pattern, func(t *testing.T) {
			policy := &Policy{Enabled: true, Rules: []Rule{{Pattern: pattern, Action: ActionDeny}}}
			t.Run("save", func(t *testing.T) {
				dir := t.TempDir()
				if err := SavePolicy(dir, &Policy{Enabled: true, Rules: []Rule{{Pattern: "sts:*", Action: ActionConfirm}}}); err != nil {
					t.Fatal(err)
				}
				before, err := os.ReadFile(PolicyFilePath(dir))
				if err != nil {
					t.Fatal(err)
				}
				if err := SavePolicy(dir, policy); err == nil || !strings.Contains(err.Error(), "service:action") {
					t.Errorf("SavePolicy error=%v, want pattern format error", err)
				}
				after, err := os.ReadFile(PolicyFilePath(dir))
				if err != nil || !bytes.Equal(before, after) {
					t.Fatalf("invalid policy changed stored configuration: %v", err)
				}
			})
			t.Run("file", func(t *testing.T) {
				dir := t.TempDir()
				data, err := json.Marshal(policy)
				if err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(PolicyFilePath(dir), data, 0600); err != nil {
					t.Fatal(err)
				}
				if _, err := LoadPolicy(dir); err == nil || !strings.Contains(err.Error(), "service:action") {
					t.Fatalf("LoadPolicy error=%v, want pattern format error", err)
				}
			})
			t.Run("environment", func(t *testing.T) {
				t.Setenv(EnvPolicyEnabled, "true")
				t.Setenv(EnvPolicyRules, pattern+"=deny")
				if _, err := LoadEffectivePolicy(t.TempDir()); err == nil || !strings.Contains(err.Error(), "service:action") {
					t.Fatalf("LoadEffectivePolicy error=%v, want pattern format error", err)
				}
			})
		})
	}
}

func TestPolicyAcceptsStructuredPatternsWithoutMetadata(t *testing.T) {
	for _, pattern := range []string{
		"sts:GetCallerIdentity", "sts:*", "*:GetCallerIdentity", "*:*",
		"RDS_MYSQL_V2:Delete*", "future-service:NewUnlistedAction", "st*:Get*Identity",
		" \tsts:GetCallerIdentity\n",
	} {
		t.Run(pattern, func(t *testing.T) {
			dir := t.TempDir()
			if err := SavePolicy(dir, &Policy{Enabled: true, Rules: []Rule{{Pattern: pattern, Action: ActionDeny}}}); err != nil {
				t.Fatal(err)
			}
			loaded, err := LoadPolicy(dir)
			if err != nil || len(loaded.Rules) != 1 || loaded.Rules[0].Pattern != strings.TrimSpace(pattern) {
				t.Fatalf("stored policy=%#v error=%v", loaded, err)
			}
			t.Setenv(EnvPolicyRules, pattern+"=deny")
			effective, err := LoadEffectivePolicy(dir)
			if err != nil || len(effective.Rules) != 1 || effective.Rules[0].Pattern != strings.TrimSpace(pattern) {
				t.Fatalf("effective policy=%#v error=%v", effective, err)
			}
		})
	}
}
