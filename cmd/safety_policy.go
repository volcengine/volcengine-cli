package cmd

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"

	"github.com/spf13/cobra"
	"github.com/volcengine/volcengine-cli/internal/safety"
)

var (
	safetyPolicyInput  io.Reader = os.Stdin
	safetyPolicyOutput io.Writer = os.Stderr
)

func newConfigureSafetyPolicyCommand() *cobra.Command {
	command := &cobra.Command{
		Use:   "safety-policy",
		Short: tr("Manage safety policy and human-in-the-loop rules"),
		Long: tr(`Configure safety policy to deny or require confirmation for destructive operations.
Rules match service:action case-insensitively, with * as a wildcard; all spellings of a service, including versioned and underscore commands, share rules.
The first matching rule wins; unmatched commands are allowed.`),
		Args: cobra.NoArgs,
		RunE: func(command *cobra.Command, _ []string) error {
			return showSafetyPolicy(command)
		},
	}
	command.AddCommand(
		newSafetyPolicyShowCommand(),
		newSafetyPolicyEnableCommand(),
		newSafetyPolicyDisableCommand(),
		newSafetyPolicyAddCommand(),
		newSafetyPolicyRemoveCommand(),
		newSafetyPolicyListCommand(),
		newSafetyPolicyPresetCommand(),
	)
	return command
}

func newSafetyPolicyShowCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "show",
		Short: tr("Display the effective safety policy"),
		Args:  cobra.NoArgs,
		RunE: func(command *cobra.Command, _ []string) error {
			return showSafetyPolicy(command)
		},
	}
}

func newSafetyPolicyEnableCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "enable",
		Short: tr("Enable the safety policy"),
		Args:  cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			dir, policy, err := loadStoredSafetyPolicy()
			if err != nil {
				return err
			}
			policy.Enabled = true
			return safety.SavePolicy(dir, policy)
		},
	}
}

func newSafetyPolicyDisableCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "disable",
		Short: tr("Disable the safety policy"),
		Args:  cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			dir, policy, err := loadStoredSafetyPolicy()
			if err != nil {
				return err
			}
			policy.Enabled = false
			return safety.SavePolicy(dir, policy)
		},
	}
}

func newSafetyPolicyAddCommand() *cobra.Command {
	var pattern, actionValue string
	command := &cobra.Command{
		Use:   "add",
		Short: tr("Add or update a safety policy rule"),
		Args:  cobra.NoArgs,
		RunE: func(added *cobra.Command, _ []string) error {
			pattern = strings.TrimSpace(pattern)
			action := safety.Action(strings.ToLower(strings.TrimSpace(actionValue)))
			if pattern == "" {
				return trErrorf("--pattern is required for add")
			}
			if action != safety.ActionDeny && action != safety.ActionConfirm {
				return trErrorf("action must be deny or confirm")
			}
			dir, policy, err := loadStoredSafetyPolicy()
			if err != nil {
				return err
			}
			// A rule for the same pattern keeps its spelling, and its position
			// when the new action is safe and effective there; otherwise it
			// moves as a new rule would.
			rule := safety.Rule{Pattern: pattern, Action: action}
			kept := policy.Rules[:0:0]
			existing := -1
			for i, current := range policy.Rules {
				if strings.EqualFold(current.Pattern, pattern) {
					existing = i
					rule.Pattern = current.Pattern
					continue
				}
				kept = append(kept, current)
			}
			var shadow *safety.Rule
			if existing >= 0 && safetyRulePositionValid(kept, existing, rule) {
				policy.Rules[existing].Action = action
			} else {
				policy.Rules, shadow = insertSafetyRule(kept, rule)
			}
			if err := safety.SavePolicy(dir, policy); err != nil {
				return err
			}
			if shadow != nil {
				fmt.Fprintf(added.ErrOrStderr(), tr("Warning: the earlier rule %s -> %s matches some of the same APIs first, so %s -> %s does not apply to them\n"), shadow.Pattern, shadow.Action, pattern, action)
			}
			return nil
		},
	}
	command.Flags().StringVar(&pattern, "pattern", "", tr("Case-insensitive service:action pattern with * wildcard"))
	command.Flags().StringVar(&actionValue, "action", "", tr("Rule action: deny or confirm"))
	return command
}

// insertSafetyRule places a new rule where it applies without loosening
// the policy, and returns the earlier rule, if any, that matches some of the
// same APIs first. The first matching rule wins, so a rule appended after a
// catch-all such as a preset's trailing *:* would never apply, and a confirm
// rule placed before a deny rule for the same APIs would let approval unlock
// what the deny blocked. A deny rule goes first. A confirm rule goes right
// after the last deny rule that matches some of the same APIs, or first when
// none does, so an unrelated deny cannot push it behind allow rules. A
// trailing *:* rule is a catch-all and stays last.
func insertSafetyRule(rules []safety.Rule, rule safety.Rule) ([]safety.Rule, *safety.Rule) {
	position := 0
	if rule.Action == safety.ActionConfirm {
		end := len(rules)
		if end > 0 && rules[end-1].Pattern == "*:*" {
			end--
		}
		for i := 0; i < end; i++ {
			if rules[i].Action == safety.ActionDeny && safetyRulesOverlap(rules[i].Pattern, rule.Pattern) {
				position = i + 1
			}
		}
	}
	var shadow *safety.Rule
	for i := 0; i < position; i++ {
		if safetyRuleHides(rules[i], rule) && safetyRulesOverlap(rules[i].Pattern, rule.Pattern) {
			shadow = &rules[i]
			break
		}
	}
	inserted := make([]safety.Rule, 0, len(rules)+1)
	inserted = append(inserted, rules[:position]...)
	inserted = append(inserted, rule)
	return append(inserted, rules[position:]...), shadow
}

// safetyRulePositionValid reports whether rule, placed at position among
// the other rules, neither hides a deny rule for the same APIs (a confirm
// rule ahead of one) nor sits behind an allow or confirm rule that matches
// the same APIs first.
func safetyRulePositionValid(others []safety.Rule, position int, rule safety.Rule) bool {
	for i, other := range others {
		if !safetyRulesOverlap(other.Pattern, rule.Pattern) {
			continue
		}
		if i < position && safetyRuleHides(other, rule) {
			return false
		}
		trailing := i == len(others)-1 && other.Pattern == "*:*"
		if i >= position && rule.Action == safety.ActionConfirm && other.Action == safety.ActionDeny && !trailing {
			return false
		}
	}
	return true
}

// safetyRuleHides reports whether earlier, matching some of the same APIs
// first, gives them a weaker action than rule: an allow ahead of a confirm,
// or an allow or confirm ahead of a deny.
func safetyRuleHides(earlier, rule safety.Rule) bool {
	switch rule.Action {
	case safety.ActionDeny:
		return earlier.Action != safety.ActionDeny
	case safety.ActionConfirm:
		return earlier.Action == safety.ActionAllow
	}
	return false
}

// safetyRulesOverlap reports whether two rule patterns match some of the
// same API calls. A pattern that names its service is compared under every
// spelling of that service, as Check matches them.
func safetyRulesOverlap(a, b string) bool {
	for _, left := range safetyPatternSpellings(a) {
		for _, right := range safetyPatternSpellings(b) {
			if safety.PatternsOverlap(left, right) {
				return true
			}
		}
	}
	return false
}

func safetyPatternSpellings(pattern string) []string {
	service, action, found := strings.Cut(strings.TrimSpace(pattern), ":")
	if !found || strings.Contains(service, "*") {
		return []string{pattern}
	}
	spellings := []string{pattern}
	for _, alias := range safetyServiceAliases(service) {
		spellings = append(spellings, alias+":"+action)
	}
	return spellings
}

func newSafetyPolicyPresetCommand() *cobra.Command {
	var overwrite, appendRules bool
	command := &cobra.Command{
		Use:   "preset <name>",
		Short: tr("Apply an official safety policy preset"),
		Long: tr(`Apply an official safety policy preset and enable the policy.
confirm-writes allows read APIs (Describe, List, Get, Query and other read verbs) and requires confirmation for everything else, including reads that issue credentials, reveal secrets, open consoles or return upload URLs.
confirm-deletes requires confirmation for destructive or irreversible operations (Delete, Remove, Terminate, Release, Restore, Reset and similar) and allows everything else.
deny-writes is confirm-writes with every confirmation turned into a denial: only read APIs run, and no approval unlocks anything else.
When rules already exist, choose --overwrite to replace them or --append to add the preset after them; an interactive terminal asks when neither is given.`),
		Args:      cobra.ExactArgs(1),
		ValidArgs: safety.PresetNames(),
		RunE: func(_ *cobra.Command, args []string) error {
			name := strings.ToLower(strings.TrimSpace(args[0]))
			rules, ok := safety.PresetRules(name)
			if !ok {
				return trErrorf("unknown safety policy preset %q; available presets: %s", args[0], strings.Join(safety.PresetNames(), ", "))
			}
			if overwrite && appendRules {
				return trErrorf("--overwrite and --append cannot be used together")
			}
			dir, policy, err := loadStoredSafetyPolicy()
			if err != nil {
				return err
			}
			if len(policy.Rules) > 0 && !overwrite && !appendRules {
				choice, err := askSafetyPolicyPresetMerge(len(policy.Rules))
				if err != nil {
					return err
				}
				overwrite, appendRules = choice == "overwrite", choice == "append"
				if !overwrite && !appendRules {
					fmt.Fprintln(safetyPolicyOutput, tr("Cancelled; the safety policy was not changed."))
					return nil
				}
			}
			if appendRules {
				rules = appendSafetyPolicyPreset(policy.Rules, rules)
			}
			policy.Rules = rules
			policy.Enabled = true
			if err := safety.SavePolicy(dir, policy); err != nil {
				return err
			}
			fmt.Fprintf(safetyPolicyOutput, tr("Applied the %s safety policy preset: %d rules, policy enabled.\n"), name, len(policy.Rules))
			return nil
		},
	}
	command.Flags().BoolVar(&overwrite, "overwrite", false, tr("Replace existing rules with the preset"))
	command.Flags().BoolVar(&appendRules, "append", false, tr("Keep existing rules and add the preset after them"))
	return command
}

// askSafetyPolicyPresetMerge asks whether to overwrite or append to the
// existing rules, and returns "overwrite", "append" or "" to cancel. Without
// a terminal it fails, so scripts and agents must choose explicitly.
func askSafetyPolicyPresetMerge(existing int) (string, error) {
	if !safety.IsInteractive(safetyPolicyInput) {
		return "", trErrorf("the safety policy already has %d rules; rerun with --overwrite to replace them or --append to keep them", existing)
	}
	fmt.Fprintf(safetyPolicyOutput, tr("The safety policy already has %d rules. [o]verwrite, [a]ppend or [c]ancel? "), existing)
	var answer string
	_, _ = fmt.Fscanln(safetyPolicyInput, &answer)
	switch strings.ToLower(strings.TrimSpace(answer)) {
	case "o", "overwrite":
		return "overwrite", nil
	case "a", "append":
		return "append", nil
	}
	return "", nil
}

// appendSafetyPolicyPreset adds the preset after the existing rules, which
// keep precedence because the first matching rule wins. Preset rules whose
// pattern already exists are skipped. An existing catch-all rule would shadow
// the whole preset, so it is reported.
func appendSafetyPolicyPreset(existing, preset []safety.Rule) []safety.Rule {
	merged := append([]safety.Rule{}, existing...)
	patterns := map[string]bool{}
	for _, rule := range existing {
		patterns[strings.ToLower(rule.Pattern)] = true
		if rule.Pattern == "*:*" {
			fmt.Fprintf(safetyPolicyOutput, tr("Warning: the existing rule %s=%s matches every operation before the preset; remove it for the preset to take effect.\n"), rule.Pattern, rule.Action)
		}
	}
	for _, rule := range preset {
		if !patterns[strings.ToLower(rule.Pattern)] {
			merged = append(merged, rule)
		}
	}
	return merged
}

func newSafetyPolicyRemoveCommand() *cobra.Command {
	var pattern string
	command := &cobra.Command{
		Use:   "remove",
		Short: tr("Remove a safety policy rule by pattern"),
		Args:  cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			pattern = strings.TrimSpace(pattern)
			if pattern == "" {
				return trErrorf("--pattern is required for remove")
			}
			dir, policy, err := loadStoredSafetyPolicy()
			if err != nil {
				return err
			}
			rules := make([]safety.Rule, 0, len(policy.Rules))
			for _, rule := range policy.Rules {
				if !strings.EqualFold(rule.Pattern, pattern) {
					rules = append(rules, rule)
				}
			}
			policy.Rules = rules
			return safety.SavePolicy(dir, policy)
		},
	}
	command.Flags().StringVar(&pattern, "pattern", "", tr("Rule pattern to remove (case-insensitive)"))
	return command
}

func newSafetyPolicyListCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: tr("List all effective safety policy rules"),
		Args:  cobra.NoArgs,
		RunE: func(command *cobra.Command, _ []string) error {
			dir, policy, err := loadEffectiveSafetyPolicy()
			if err != nil {
				return err
			}
			status := tr("disabled")
			if policy.Enabled {
				status = tr("enabled")
			}
			fmt.Fprintf(command.OutOrStdout(), "%s: %s\n", tr("Safety policy"), status)
			fmt.Fprintf(command.OutOrStdout(), "%s: %s\n\n", tr("Config file"), safety.PolicyFilePath(dir))
			if len(policy.Rules) == 0 {
				fmt.Fprintln(command.OutOrStdout(), tr("No rules configured."))
				return nil
			}
			fmt.Fprintln(command.OutOrStdout(), tr("Rules:"))
			for i, rule := range policy.Rules {
				fmt.Fprintf(command.OutOrStdout(), "  %d. %s -> %s\n", i+1, rule.Pattern, rule.Action)
			}
			return nil
		},
	}
}

func showSafetyPolicy(command *cobra.Command) error {
	_, policy, err := loadEffectiveSafetyPolicy()
	if err != nil {
		return err
	}
	data, err := json.MarshalIndent(policy, "", "  ")
	if err != nil {
		return err
	}
	_, err = fmt.Fprintln(command.OutOrStdout(), string(data))
	return err
}

func safetyPolicyConfigDir() (string, error) {
	dir, err := configFileDirFunc()
	if err != nil {
		return "", fmt.Errorf("resolve safety policy config directory: %w", err)
	}
	return dir, nil
}

func loadStoredSafetyPolicy() (string, *safety.Policy, error) {
	dir, err := safetyPolicyConfigDir()
	if err != nil {
		return "", nil, err
	}
	policy, err := safety.LoadPolicy(dir)
	if err != nil {
		return "", nil, fmt.Errorf("load safety policy: %w", err)
	}
	return dir, policy, nil
}

func loadEffectiveSafetyPolicy() (string, *safety.Policy, error) {
	dir, err := safetyPolicyConfigDir()
	if err != nil {
		return "", nil, err
	}
	policy, err := safety.LoadEffectivePolicy(dir)
	if err != nil {
		return "", nil, fmt.Errorf("load effective safety policy: %w", err)
	}
	return dir, policy, nil
}

func safetyConfirmationSkipped(callContext *Context) bool {
	if safety.SkipConfirmFromEnvironment() {
		return true
	}
	return callContext != nil && callContext.fixedFlags != nil && callContext.fixedFlags.GetByName("yes") != nil
}

func enforceSafetyPolicy(product, action string, skipConfirm, interactive bool, retryInstruction string) error {
	_, policy, err := loadEffectiveSafetyPolicy()
	if err != nil {
		return err
	}
	return enforceGivenSafetyPolicy(policy, product, action, skipConfirm, interactive, retryInstruction,
		tr("operation blocked by safety policy: %s %s (rule: %s)"))
}

// enforceGivenSafetyPolicy is enforceSafetyPolicy with an already resolved
// policy. denied formats a deny with the product, action and matched rule
// pattern; messages for a model use explicit argument indexes to leave the
// pattern out.
func enforceGivenSafetyPolicy(policy *safety.Policy, product, action string, skipConfirm, interactive bool, retryInstruction, denied string) error {
	return safety.CheckAndConfirmWithMessages(
		safetyPolicyInput,
		safetyPolicyOutput,
		interactive,
		policy,
		safety.CommandInfo{Product: product, ProductAliases: safetyServiceAliases(product), Action: action},
		skipConfirm || safety.SkipConfirmFromEnvironment(),
		retryInstruction,
		safety.Messages{
			Denied: denied,
			// Keep this literal for the static i18n catalog scanner. A regression
			// test checks it against safety.ConfirmationNeededMessage.
			ConfirmationNeeded:  tr("safety policy requires confirmation for: %s %s\ncurrent environment is non-interactive.\nSTOP: Briefly explain the target and impact, ask to continue, then end this turn and wait for a NEW user reply.\nOnly after approval, internally %s ONCE with unchanged arguments; do not ask the user to handle this flag.\nDiscard approval after that attempt, even on failure. Start every new operation without --yes/yes=true; explain and ask only if blocked. Never reuse earlier approvals or weaken policy."),
			Prompt:              tr("Safety policy requires confirmation for: %s %s\nType 'yes' to proceed, anything else to cancel: "),
			InputUnavailable:    tr("operation cancelled: confirmation input is unavailable"),
			ConfirmationReadErr: tr("operation cancelled: reading confirmation: %v"),
			Cancelled:           tr("operation cancelled by user"),
		},
	)
}

// safetyServiceAliases returns the other spellings of the same SDK service:
// its versioned commands, underscore-compatible commands and the SDK service
// name itself, so CLI and MCP calls share the same rules.
func safetyServiceAliases(service string) []string {
	service = strings.ToLower(strings.TrimSpace(service))
	sdkService := safetySDKService(service)
	if sdkService == "" {
		return nil
	}
	spellings := map[string]struct{}{sdkService: {}}
	for command, owner := range serviceMapping {
		if owner == sdkService {
			spellings[command] = struct{}{}
		}
	}
	for _, name := range compatible_support_cmd {
		if safetySDKService(name) == sdkService {
			spellings[name] = struct{}{}
		}
	}
	delete(spellings, service)
	aliases := make([]string, 0, len(spellings))
	for spelling := range spellings {
		aliases = append(aliases, spelling)
	}
	sort.Strings(aliases)
	return aliases
}

func safetySDKService(service string) string {
	if sdkService, ok := serviceMapping[service]; ok {
		return sdkService
	}
	for _, name := range compatible_support_cmd {
		if name == service {
			return serviceMapping[strings.ReplaceAll(name, "_", "")]
		}
	}
	for _, sdkService := range serviceMapping {
		if sdkService == service {
			return sdkService
		}
	}
	return ""
}

// safetyDeniedForModel formats a safety policy deny returned to a model by
// call_api and run_script. It leaves out the matched rule: a catch-all
// pattern such as *:* reads as if every API were blocked, and the model
// cannot act on it. CLI users, who own the policy, still see the rule.
func safetyDeniedForModel() string {
	return tr("operation blocked by safety policy: %[1]s %[2]s. Do not retry it or try another way to perform it; tell the user it was blocked.")
}

func enforceCLISafetyPolicy(callContext *Context, product, action string) error {
	return enforceSafetyPolicy(
		product,
		action,
		safetyConfirmationSkipped(callContext),
		safety.IsInteractive(safetyPolicyInput),
		tr("re-run the same command with --yes"),
	)
}
