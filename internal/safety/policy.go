package safety

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

type Action string

const (
	ActionAllow   Action = "allow"
	ActionDeny    Action = "deny"
	ActionConfirm Action = "confirm"
)

const (
	PolicyFileName   = "safety-policy.json"
	EnvPolicyEnabled = "VOLCENGINE_SAFETY_POLICY_ENABLED"
	EnvPolicyRules   = "VOLCENGINE_SAFETY_POLICY_RULES"
	EnvSkipConfirm   = "VOLCENGINE_SAFETY_SKIP_CONFIRM"
)

type Rule struct {
	Pattern string `json:"pattern"`
	Action  Action `json:"action"`
}

type Policy struct {
	Enabled bool   `json:"enabled"`
	Rules   []Rule `json:"rules"`
}

type CommandInfo struct {
	Product string
	// ProductAliases are other spellings of the same service. A rule written
	// with any of them applies to Product.
	ProductAliases []string
	Action         string
}

type CheckResult struct {
	Action  Action
	Matched bool
	Rule    *Rule
}

func DefaultPolicy() *Policy {
	return &Policy{Rules: []Rule{}}
}

func (p *Policy) Check(command CommandInfo) CheckResult {
	if p == nil || !p.Enabled || len(p.Rules) == 0 {
		return CheckResult{Action: ActionAllow}
	}

	identifiers := commandIdentifiers(command)
	for i := range p.Rules {
		rule := &p.Rules[i]
		if !matchAnyIdentifier(rule.Pattern, identifiers) {
			continue
		}
		action := rule.Action
		if !validRuntimeAction(action) {
			// An invalid in-memory policy must never weaken protection.
			action = ActionDeny
		}
		return CheckResult{Action: action, Matched: true, Rule: rule}
	}
	return CheckResult{Action: ActionAllow}
}

func commandIdentifiers(command CommandInfo) []string {
	action := strings.TrimSpace(command.Action)
	identifiers := make([]string, 0, 1+len(command.ProductAliases))
	for _, product := range append([]string{command.Product}, command.ProductAliases...) {
		identifiers = append(identifiers, strings.ToLower(strings.TrimSpace(product))+":"+action)
	}
	return identifiers
}

func matchAnyIdentifier(pattern string, identifiers []string) bool {
	for _, identifier := range identifiers {
		if matchPattern(pattern, identifier) {
			return true
		}
	}
	return false
}

func validRuntimeAction(action Action) bool {
	return action == ActionAllow || action == ActionDeny || action == ActionConfirm
}

func matchPattern(pattern, command string) bool {
	pattern = strings.TrimSpace(pattern)
	command = strings.TrimSpace(command)
	if pattern == "" {
		return false
	}
	quoted := regexp.QuoteMeta(pattern)
	quoted = strings.ReplaceAll(quoted, `\*`, ".*")
	re, err := regexp.Compile("(?i)^" + quoted + "$")
	return err == nil && re.MatchString(command)
}

func PolicyFilePath(configDir string) string {
	return filepath.Join(configDir, PolicyFileName)
}

func LoadPolicy(configDir string) (*Policy, error) {
	data, err := os.ReadFile(PolicyFilePath(configDir))
	if err != nil {
		if os.IsNotExist(err) {
			return DefaultPolicy(), nil
		}
		return nil, err
	}

	var policy *Policy
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&policy); err != nil {
		return nil, fmt.Errorf("parse safety policy: %w", err)
	}
	if policy == nil {
		return nil, fmt.Errorf("parse safety policy: expected an object, got null")
	}
	var extra interface{}
	if err := decoder.Decode(&extra); err == nil {
		return nil, fmt.Errorf("parse safety policy: multiple JSON values")
	} else if err != io.EOF {
		return nil, fmt.Errorf("parse safety policy: trailing data: %w", err)
	}
	if policy.Rules == nil {
		policy.Rules = []Rule{}
	}
	if err := validatePolicy(policy); err != nil {
		return nil, fmt.Errorf("validate safety policy: %w", err)
	}
	return policy, nil
}

func LoadEffectivePolicy(configDir string) (*Policy, error) {
	policy, err := LoadPolicy(configDir)
	if err != nil {
		return nil, err
	}
	return mergePolicyFromEnvironment(policy)
}

func mergePolicyFromEnvironment(base *Policy) (*Policy, error) {
	if base == nil {
		base = DefaultPolicy()
	}
	if err := validatePolicy(base); err != nil {
		return nil, err
	}

	enabled := base.Enabled
	if value, ok := os.LookupEnv(EnvPolicyEnabled); ok {
		parsed, err := strconv.ParseBool(strings.TrimSpace(value))
		if err != nil {
			return nil, fmt.Errorf("%s: invalid boolean %q", EnvPolicyEnabled, value)
		}
		enabled = parsed
	}

	rules := cloneRules(base.Rules)
	if value, ok := os.LookupEnv(EnvPolicyRules); ok {
		value = strings.TrimSpace(value)
		if value == "" {
			rules = []Rule{}
		} else {
			parsed, err := parseEnvironmentRules(value)
			if err != nil {
				return nil, fmt.Errorf("%s: %w", EnvPolicyRules, err)
			}
			rules = parsed
		}
	}
	return &Policy{Enabled: enabled, Rules: rules}, nil
}

func cloneRules(rules []Rule) []Rule {
	if len(rules) == 0 {
		return []Rule{}
	}
	return append([]Rule(nil), rules...)
}

func parseEnvironmentRules(value string) ([]Rule, error) {
	parts := strings.Split(value, ",")
	rules := make([]Rule, 0, len(parts))
	for i, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			return nil, fmt.Errorf("entry %d is empty", i+1)
		}
		pattern, actionValue, ok := strings.Cut(part, "=")
		if !ok {
			return nil, fmt.Errorf("entry %d must use pattern=action", i+1)
		}
		pattern = strings.TrimSpace(pattern)
		action := normalizeAction(Action(actionValue))
		if pattern == "" {
			return nil, fmt.Errorf("entry %d has an empty pattern", i+1)
		}
		if !validConfiguredAction(action) {
			return nil, fmt.Errorf("entry %d has invalid action %q", i+1, strings.TrimSpace(actionValue))
		}
		rules = append(rules, Rule{Pattern: pattern, Action: action})
	}
	return rules, nil
}

func validatePolicy(policy *Policy) error {
	if policy == nil {
		return fmt.Errorf("policy is nil")
	}
	for i := range policy.Rules {
		policy.Rules[i].Pattern = strings.TrimSpace(policy.Rules[i].Pattern)
		policy.Rules[i].Action = normalizeAction(policy.Rules[i].Action)
		if policy.Rules[i].Pattern == "" {
			return fmt.Errorf("rules[%d].pattern is empty", i)
		}
		if !validConfiguredAction(policy.Rules[i].Action) {
			return fmt.Errorf("rules[%d].action %q is invalid", i, policy.Rules[i].Action)
		}
	}
	return nil
}

func normalizeAction(action Action) Action {
	return Action(strings.ToLower(strings.TrimSpace(string(action))))
}

func validConfiguredAction(action Action) bool {
	return validRuntimeAction(action)
}

func SavePolicy(configDir string, policy *Policy) error {
	if policy == nil {
		policy = DefaultPolicy()
	}
	if err := validatePolicy(policy); err != nil {
		return err
	}
	if err := os.MkdirAll(configDir, 0o700); err != nil {
		return err
	}
	_ = os.Chmod(configDir, 0o700)

	data, err := json.MarshalIndent(policy, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')

	temp, err := os.CreateTemp(configDir, ".tmp-safety-policy-*")
	if err != nil {
		return err
	}
	tempName := temp.Name()
	defer func() {
		_ = temp.Close()
		_ = os.Remove(tempName)
	}()
	if err := temp.Chmod(0o600); err != nil {
		return err
	}
	if _, err := temp.Write(data); err != nil {
		return err
	}
	if err := temp.Close(); err != nil {
		return err
	}

	target := PolicyFilePath(configDir)
	if err := os.Rename(tempName, target); err != nil {
		// Windows does not replace an existing destination atomically.
		if removeErr := os.Remove(target); removeErr != nil && !os.IsNotExist(removeErr) {
			return err
		}
		if secondErr := os.Rename(tempName, target); secondErr != nil {
			return secondErr
		}
	}
	return os.Chmod(target, 0o600)
}
