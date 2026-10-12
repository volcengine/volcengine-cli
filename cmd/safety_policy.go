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
		RunE: func(_ *cobra.Command, _ []string) error {
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
			for i := range policy.Rules {
				if strings.EqualFold(policy.Rules[i].Pattern, pattern) {
					policy.Rules[i].Action = action
					return safety.SavePolicy(dir, policy)
				}
			}
			policy.Rules = append(policy.Rules, safety.Rule{Pattern: pattern, Action: action})
			return safety.SavePolicy(dir, policy)
		},
	}
	command.Flags().StringVar(&pattern, "pattern", "", tr("Case-insensitive service:action pattern with * wildcard"))
	command.Flags().StringVar(&actionValue, "action", "", tr("Rule action: deny or confirm"))
	return command
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
	return safety.CheckAndConfirmWithMessages(
		safetyPolicyInput,
		safetyPolicyOutput,
		interactive,
		policy,
		safety.CommandInfo{Product: product, ProductAliases: safetyServiceAliases(product), Action: action},
		skipConfirm || safety.SkipConfirmFromEnvironment(),
		retryInstruction,
		safety.Messages{
			Denied: tr("operation blocked by safety policy: %s %s (rule: %s)"),
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

func enforceCLISafetyPolicy(callContext *Context, product, action string) error {
	return enforceSafetyPolicy(
		product,
		action,
		safetyConfirmationSkipped(callContext),
		safety.IsInteractive(safetyPolicyInput),
		tr("re-run the same command with --yes"),
	)
}
