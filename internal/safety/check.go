package safety

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/chzyer/readline"
)

// ConfirmationNeededMessage is shared by the default and localized CLI/MCP errors.
const ConfirmationNeededMessage = "safety policy requires confirmation for: %s %s\ncurrent environment is non-interactive.\nSTOP: Briefly explain the target and impact, ask to continue, then end this turn and wait for a NEW user reply.\nOnly after approval, internally %s ONCE with unchanged arguments; do not ask the user to handle this flag.\nDiscard approval after that attempt, even on failure. Start every new operation without --yes/yes=true; explain and ask only if blocked. Never reuse earlier approvals or weaken policy."

// MCPApprovalDescription describes the same approval flow for tool discovery,
// before a confirmation error exists. It is guidance, not a one-use token.
const MCPApprovalDescription = "Omit on every new operation. Ask only after a block; wait for a new user reply approving it, then use internally for ONE unchanged retry. Discard approval even on failure. Do not ask the user to handle this flag. Cannot bypass deny."

type Messages struct {
	Denied              string
	ConfirmationNeeded  string
	Prompt              string
	InputUnavailable    string
	ConfirmationReadErr string
	Cancelled           string
}

func CheckAndConfirm(input io.Reader, output io.Writer, interactive bool, policy *Policy, command CommandInfo, skipConfirm bool, retryInstruction string) error {
	return CheckAndConfirmWithMessages(input, output, interactive, policy, command, skipConfirm, retryInstruction, Messages{})
}

func CheckAndConfirmWithMessages(input io.Reader, output io.Writer, interactive bool, policy *Policy, command CommandInfo, skipConfirm bool, retryInstruction string, messages Messages) error {
	messages = messages.withDefaults()
	result := policy.Check(command)
	switch result.Action {
	case ActionDeny:
		return fmt.Errorf(messages.Denied, command.Product, command.Action, result.Rule.Pattern)
	case ActionConfirm:
		if skipConfirm {
			return nil
		}
		if !interactive {
			if strings.TrimSpace(retryInstruction) == "" {
				retryInstruction = "re-run the same command with --yes"
			}
			return fmt.Errorf(messages.ConfirmationNeeded, command.Product, command.Action, retryInstruction)
		}
		if output == nil {
			output = io.Discard
		}
		_, _ = fmt.Fprintf(output, messages.Prompt, command.Product, command.Action)
		if input == nil {
			return errors.New(messages.InputUnavailable)
		}
		line, err := bufio.NewReader(input).ReadString('\n')
		if err != nil {
			return fmt.Errorf(messages.ConfirmationReadErr, err)
		}
		answer := strings.ToLower(strings.TrimSpace(line))
		if answer != "y" && answer != "yes" {
			return errors.New(messages.Cancelled)
		}
	}
	return nil
}

func (messages Messages) withDefaults() Messages {
	if messages.Denied == "" {
		messages.Denied = "operation blocked by safety policy: %s %s (rule: %s)"
	}
	if messages.ConfirmationNeeded == "" {
		messages.ConfirmationNeeded = ConfirmationNeededMessage
	}
	if messages.Prompt == "" {
		messages.Prompt = "Safety policy requires confirmation for: %s %s\nType 'yes' to proceed, anything else to cancel: "
	}
	if messages.InputUnavailable == "" {
		messages.InputUnavailable = "operation cancelled: confirmation input is unavailable"
	}
	if messages.ConfirmationReadErr == "" {
		messages.ConfirmationReadErr = "operation cancelled: reading confirmation: %v"
	}
	if messages.Cancelled == "" {
		messages.Cancelled = "operation cancelled by user"
	}
	return messages
}

func IsInteractive(input io.Reader) bool {
	file, ok := input.(*os.File)
	if !ok {
		return false
	}
	return readline.IsTerminal(int(file.Fd()))
}

func SkipConfirmFromEnvironment() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(EnvSkipConfirm))) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}
