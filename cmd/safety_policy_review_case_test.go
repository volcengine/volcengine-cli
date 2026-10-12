package cmd

import (
	"os"
	"reflect"
	"testing"

	"github.com/volcengine/volcengine-cli/internal/safety"
)

func TestReviewSafetyPolicyAddCaseInsensitiveUpdate(t *testing.T) {
	dir := reviewSafetyEnvironment(t)
	for _, args := range [][]string{
		{"enable"},
		{"add", "--pattern", "ecs:delete*", "--action", "confirm"},
		{"add", "--pattern", "ECS:Delete*", "--action", "deny"},
	} {
		if _, err := runSafetyPolicyCommandForTest(t, args...); err != nil {
			t.Fatal(err)
		}
	}
	policy, err := safety.LoadEffectivePolicy(dir)
	if err != nil {
		t.Fatal(err)
	}
	result := policy.Check(safety.CommandInfo{Product: "ecs", Action: "DeleteInstance"})
	t.Logf("rules=%v effective action=%s", policy.Rules, result.Action)
	if len(policy.Rules) != 1 || result.Action != safety.ActionDeny {
		t.Fatal("case-only update must replace the existing rule and make deny effective")
	}
}

func TestReviewSafetyPolicyRemoveCaseInsensitiveMatch(t *testing.T) {
	dir := reviewSafetyEnvironment(t)
	for _, args := range [][]string{
		{"enable"},
		{"add", "--pattern", "ecs:delete*", "--action", "confirm"},
		{"add", "--pattern", "ECS:Delete*", "--action", "deny"},
	} {
		if _, err := runSafetyPolicyCommandForTest(t, args...); err != nil {
			t.Fatal(err)
		}
	}
	_, removeErr := runSafetyPolicyCommandForTest(t, "remove", "--pattern", "ECS:DELETE*")
	policy, err := safety.LoadEffectivePolicy(dir)
	if err != nil {
		t.Fatal(err)
	}
	result := policy.Check(safety.CommandInfo{Product: "ecs", Action: "DeleteInstance"})
	t.Logf("remove error=%v retained rules=%v effective action=%s", removeErr, policy.Rules, result.Action)
	if removeErr != nil || len(policy.Rules) != 0 || result.Action != safety.ActionAllow {
		t.Fatal("case-insensitive removal must remove the matching rules")
	}
}

func TestReviewSafetyPolicyRemoveMissingRemainsSuccessful(t *testing.T) {
	dir := reviewSafetyEnvironment(t)
	if _, err := runSafetyPolicyCommandForTest(t, "add", "--pattern", "ecs:delete*", "--action", "confirm"); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(safety.PolicyFilePath(dir))
	if err != nil {
		t.Fatal(err)
	}
	_, removeErr := runSafetyPolicyCommandForTest(t, "remove", "--pattern", "sts:*")
	after, err := os.ReadFile(safety.PolicyFilePath(dir))
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("remove missing rule: error=%v content unchanged=%v", removeErr, string(before) == string(after))
	if removeErr != nil {
		t.Errorf("removing a nonexistent rule must remain successful: %v", removeErr)
	}
	if string(before) != string(after) {
		t.Error("removing a nonexistent rule must not change existing rules")
	}
}

func TestSafetyPolicyCaseUpdatePreservesOrderAndSpelling(t *testing.T) {
	dir := reviewSafetyEnvironment(t)
	policy := &safety.Policy{Enabled: true, Rules: []safety.Rule{
		{Pattern: "ecs:Describe*", Action: safety.ActionAllow},
		{Pattern: "ecs:delete*", Action: safety.ActionConfirm},
		{Pattern: "ecs:*", Action: safety.ActionDeny},
	}}
	if err := safety.SavePolicy(dir, policy); err != nil {
		t.Fatal(err)
	}
	if _, err := runSafetyPolicyCommandForTest(t, "add", "--pattern", "ECS:Delete*", "--action", "deny"); err != nil {
		t.Fatal(err)
	}
	got, err := safety.LoadPolicy(dir)
	if err != nil {
		t.Fatal(err)
	}
	policy.Rules[1].Action = safety.ActionDeny
	if !reflect.DeepEqual(got, policy) {
		t.Fatalf("case update changed rule order or spelling: got=%v want=%v", got.Rules, policy.Rules)
	}
}

func TestSafetyPolicyCaseRemovalHandlesExistingVariants(t *testing.T) {
	dir := reviewSafetyEnvironment(t)
	policy := &safety.Policy{Enabled: true, Rules: []safety.Rule{
		{Pattern: "ecs:delete*", Action: safety.ActionConfirm},
		{Pattern: "ECS:Delete*", Action: safety.ActionDeny},
		{Pattern: "ecs:DeleteInstance", Action: safety.ActionDeny},
		{Pattern: "sts:*", Action: safety.ActionConfirm},
	}}
	if err := safety.SavePolicy(dir, policy); err != nil {
		t.Fatal(err)
	}
	if _, err := runSafetyPolicyCommandForTest(t, "remove", "--pattern", "ECS:DELETE*"); err != nil {
		t.Fatal(err)
	}
	got, err := safety.LoadPolicy(dir)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got.Rules, policy.Rules[2:]) {
		t.Fatalf("remove must compare whole patterns, ignoring only case: got=%v want=%v", got.Rules, policy.Rules[2:])
	}
}
