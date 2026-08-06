package cmd

import (
	"fmt"
	"sort"
	"strings"

	"github.com/spf13/cobra"
)

const actionHelpDetailAnnotation = "ve-help-detail"

// parseActionHelpArgs detects -h/--help and optional --detail.
// --detail alone does not trigger help (it may be a business parameter with a value).
func parseActionHelpArgs(args []string) (wantHelp, detail bool) {
	for _, a := range args {
		switch a {
		case "-h", "--help":
			wantHelp = true
		case "--detail":
			detail = true
		}
	}
	if !wantHelp {
		return false, false
	}
	return true, detail
}

// bareDetailWithoutValue reports a lone --detail token with no following value.
// That is almost always a mistaken help attempt (should be -h --detail), not a valid API call.
// --detail <value> is left alone so a real API field can still be set (including values like -1).
// Flag detection matches the invocation parser: only -- / --- prefixes are flags (not single-dash tokens).
func bareDetailWithoutValue(args []string) bool {
	for i, a := range args {
		if a != "--detail" {
			continue
		}
		if i+1 >= len(args) {
			return true
		}
		next := args[i+1]
		// Align with parser.parseArg: only -- and --- start flags.
		if strings.HasPrefix(next, "--") {
			return true
		}
		return false
	}
	return false
}

func errBareDetailWithoutHelp() error {
	return fmt.Errorf("%s", tr("--detail only expands help when used with -h/--help. Use -h --detail (or --help --detail). If you meant an API parameter named Detail, pass a value with matching case: --Detail <value>."))
}

func setActionHelpDetail(cmd *cobra.Command, detail bool) {
	if cmd.Annotations == nil {
		cmd.Annotations = map[string]string{}
	}
	if detail {
		cmd.Annotations[actionHelpDetailAnnotation] = "1"
		return
	}
	delete(cmd.Annotations, actionHelpDetailAnnotation)
}

func getActionHelpDetail(cmd *cobra.Command) bool {
	if cmd == nil || cmd.Annotations == nil {
		return false
	}
	return cmd.Annotations[actionHelpDetailAnnotation] == "1"
}

func copyParams(params []param) []param {
	if len(params) == 0 {
		return nil
	}
	out := make([]param, len(params))
	copy(out, params)
	return out
}

// buildActionHelpParamLines builds the Available Parameters lines for Action help.
// prefixLines (e.g. body skeleton for JSON APIs) are always kept first and unsorted.
// API params are sorted by key before formatting so multi-line detail blocks and
// blank separators stay adjacent (do not sort already-rendered multi-line strings).
// detail=false skips description corpus; detail=true attaches and formats full text.
func buildActionHelpParamLines(service, action string, params []param, prefixLines []string, detail bool) []string {
	ordered := copyParams(params)
	sort.Slice(ordered, func(i, j int) bool {
		return ordered[i].key < ordered[j].key
	})
	if !detail {
		return append(append([]string{}, prefixLines...), formatParamsHelpUsage(ordered, false)...)
	}
	attachParamDescriptions(service, action, ordered)
	return append(append([]string{}, prefixLines...), formatParamsHelpUsage(ordered, true)...)
}

// setLazyActionUsage installs a UsageFunc that renders action help on demand.
// Template is rebuilt per Usage() call: concise path is cheap; detail path reuses
// the process-wide Once-loaded param description corpus. Dual-mode caching is
// unnecessary for a single-process CLI and was removed to avoid stale i18n.
func setLazyActionUsage(actionCmd *cobra.Command, buildParams func(detail bool) []string) {
	defaultUsage := rootCmd.UsageFunc()
	actionCmd.SetUsageFunc(func(cmd *cobra.Command) error {
		detail := getActionHelpDetail(cmd)
		// Escape Long the same way as param prose so OpenAPI text with {{ }} cannot break Usage templates.
		long := escapeCobraTemplateLiteral(normalizeHelpDescription(cmd.Long))
		cmd.SetUsageTemplate(actionUsageTemplate(long, buildParams(detail), detail))
		return defaultUsage(cmd)
	})
}

func actionUsageTemplate(description string, params []string, detail bool) string {
	// Copy before mutating so callers can reuse the source slice.
	// Do NOT sort here: params may already be multi-line (detail) or intentionally
	// prefixed (JSON body skeleton). Callers sort by key before formatting.
	params = append([]string(nil), params...)
	for i := 0; i < len(params); i++ {
		params[i] = "  --" + params[i]
	}

	description = strings.TrimSpace(description)
	if description != "" {
		description += "\n\n"
	}

	detailTip := ""
	if !detail {
		// Default -h is intentionally concise; show the working combination for full text.
		detailTip = "\n" + tr("Default help is concise. For full parameter descriptions and examples: -h --detail (or --help --detail).") + "\n"
	}

	return fmt.Sprintf(`%s%s{{if .Runnable}}
  {{.CommandPath}} [params]{{end}}{{if .HasExample}}

%s
{{.Example}}{{end}}

%s
%s
%s
%s
%s

`, description, tr("Usage:"), tr("Examples:"), tr("Available Parameters:"), strings.Join(params, "\n"),
		detailTip,
		tr("CLI Control Flags:"),
		localizedFixedFlagsHelp())
}
