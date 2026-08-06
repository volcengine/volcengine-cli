package cmd

import (
	"fmt"
	"sort"
	"strings"

	"github.com/spf13/cobra"
)

const actionHelpDetailAnnotation = "ve-help-detail"

// isCLIFlagToken reports tokens that start a new CLI/API flag (-- or ---),
// matching parser.parseArg (single-dash tokens are values, not flags).
func isCLIFlagToken(a string) bool {
	return strings.HasPrefix(a, "--")
}

// parseActionHelpArgs detects bare -h/--help switches and optional --detail.
// --detail alone does not trigger help (it may be a business parameter with a value).
//
// Tokens that are values of a preceding value-taking flag are not treated as help
// switches (same consumption rules as the invocation parser). Example:
//
//	--Description -h   → -h is a value, not help
//	--Description --help → --help is a flag (and help); previous flag is missing its value
//	-h / --help        → help
func parseActionHelpArgs(args []string) (wantHelp, detail bool) {
	expectValue := false
	for _, a := range args {
		if expectValue {
			if isCLIFlagToken(a) {
				// Previous flag is missing a value; re-process this token as a flag.
				expectValue = false
			} else {
				// Consumed as a flag value (including a literal "-h").
				expectValue = false
				continue
			}
		}
		switch a {
		case "-h", "--help":
			wantHelp = true
			continue
		case "--detail":
			// Help-mode control when paired with -h/--help; does not consume the next
			// token so `--detail --help` still enables detail help.
			detail = true
			continue
		}
		if !isCLIFlagToken(a) {
			continue
		}
		if strings.HasPrefix(a, "---") {
			name := a[3:]
			if name == "" {
				continue
			}
			if strings.Contains(name, "=") {
				continue
			}
			if isPresenceOnlyFixedFlag(name) {
				continue
			}
			expectValue = true
			continue
		}
		// Double-dash API / reserved control flag.
		body := a[2:]
		if body == "" || strings.Contains(body, "=") {
			continue
		}
		expectValue = true
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
		// This --detail has a value; keep scanning for a later bare --detail.
		continue
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

// buildActionHelpParamLines builds Available Parameters lines (without leading "--").
// prefixLines stay first and unsorted (legacy single-list callers). Prefer
// jsonActionUsageTemplate + separate bodyParam for JSON dual-form help.
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

// setLazyActionUsage installs a UsageFunc that renders non-JSON action help on demand.
// Template is rebuilt per Usage() call: concise path is cheap; detail path reuses
// the process-wide Once-loaded param description corpus.
func setLazyActionUsage(actionCmd *cobra.Command, buildParams func(detail bool) []string) {
	defaultUsage := rootCmd.UsageFunc()
	actionCmd.SetUsageFunc(func(cmd *cobra.Command) error {
		detail := getActionHelpDetail(cmd)
		long := escapeCobraTemplateLiteral(normalizeHelpDescription(cmd.Long))
		cmd.SetUsageTemplate(actionUsageTemplate(long, buildParams(detail), detail))
		return defaultUsage(cmd)
	})
}

// setLazyJSONActionUsage is the JSON-action counterpart: Parameter Form + JSON Form
// (master layout) with the same lazy/detail behavior as setLazyActionUsage.
// bodyParam is the raw body skeleton line without a leading "--" (e.g. body '{...}').
func setLazyJSONActionUsage(actionCmd *cobra.Command, bodyParam string, buildParams func(detail bool) []string) {
	defaultUsage := rootCmd.UsageFunc()
	actionCmd.SetUsageFunc(func(cmd *cobra.Command) error {
		detail := getActionHelpDetail(cmd)
		long := escapeCobraTemplateLiteral(normalizeHelpDescription(cmd.Long))
		cmd.SetUsageTemplate(jsonActionUsageTemplate(long, buildParams(detail), bodyParam, detail))
		return defaultUsage(cmd)
	})
}

// nonJSONUsageParamIndent is the first-line indent for non-JSON actionUsageTemplate
// ("  --key"). formatParamsHelpUsage detail continuations assume this yields a 4-column prefix.
const nonJSONUsageParamIndent = "  "

// jsonSectionUsageParamIndent is the first-line indent under Parameter/JSON Form
// ("    --key"). 2 spaces deeper than nonJSONUsageParamIndent.
const jsonSectionUsageParamIndent = "    "

// actionUsageTemplate is the single-list help template (non-JSON actions).
// params entries are meta lines without a leading "--" (may be multi-line in detail mode).
func actionUsageTemplate(description string, params []string, detail bool) string {
	// Keep caller order (already key-sorted); do not re-sort multi-line detail blocks.
	parameterHelp := formatParamUsageEntries(params, nonJSONUsageParamIndent)
	return renderActionUsageTemplate(description, parameterHelp, detail)
}

// jsonActionUsageTemplate is master's dual-section layout for application/json actions:
// Parameter Form (flattened flags) + JSON Form (--body skeleton).
// detail controls the concise tip; param descriptions are already in params when detail.
func jsonActionUsageTemplate(description string, params []string, bodyParam string, detail bool) string {
	sections := make([]string, 0, 2)
	if len(params) > 0 {
		// Sort concise/meta keys like master (Filter before PageSize).
		sections = append(sections, fmt.Sprintf("  %s\n%s", tr("Parameter Form:"), formatActionUsageParams(params, jsonSectionUsageParamIndent)))
	}
	if bodyParam != "" {
		// Body skeleton is pretty-printed with relative indents; re-indent every line.
		sections = append(sections, fmt.Sprintf("  %s\n%s", tr("JSON Form:"), formatBodyUsageEntry(bodyParam, jsonSectionUsageParamIndent)))
	}
	parameterHelp := strings.Join(sections, "\n\n")
	if parameterHelp != "" {
		parameterHelp = "\n" + parameterHelp
	}
	return renderActionUsageTemplate(description, parameterHelp, detail)
}

// formatActionUsageParams sorts params then prefixes each entry (master alphabetical Parameter Form).
func formatActionUsageParams(params []string, indent string) string {
	formatted := append([]string(nil), params...)
	sort.Strings(formatted)
	return formatParamUsageEntries(formatted, indent)
}

// formatParamUsageEntries prefixes each formatParamsHelpUsage entry with indent+"--".
//
// formatParamsHelpUsage (detail mode) embeds absolute continuation indents assuming a
// first-line prefix of "  --" (4 columns). We only prefix the first line of each entry
// and, when indent is deeper than nonJSONUsageParamIndent, pad continuations by the
// extra depth so description columns stay aligned under Parameter Form.
func formatParamUsageEntries(params []string, indent string) string {
	if len(params) == 0 {
		return ""
	}
	// Extra spaces when section content is deeper than the "  --" base assumed by formatParamsHelpUsage.
	extraContPad := ""
	if len(indent) > len(nonJSONUsageParamIndent) {
		extraContPad = strings.Repeat(" ", len(indent)-len(nonJSONUsageParamIndent))
	}
	formatted := make([]string, len(params))
	for i, p := range params {
		lines := strings.Split(p, "\n")
		lines[0] = indent + "--" + lines[0]
		if extraContPad != "" {
			for j := 1; j < len(lines); j++ {
				lines[j] = extraContPad + lines[j]
			}
		}
		formatted[i] = strings.Join(lines, "\n")
	}
	return strings.Join(formatted, "\n")
}

// formatBodyUsageEntry prefixes a JSON body skeleton with indent+"--" and re-indents
// every continuation line (pretty-printed JSON uses relative whitespace, not descColIndent).
func formatBodyUsageEntry(bodyParam, indent string) string {
	if bodyParam == "" {
		return ""
	}
	param := "--" + bodyParam
	return indent + strings.ReplaceAll(param, "\n", "\n"+indent)
}

func renderActionUsageTemplate(description, parameterHelp string, detail bool) string {
	description = strings.TrimSpace(description)
	if description != "" {
		description += "\n\n"
	}

	detailTip := ""
	if !detail {
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

`, description, tr("Usage:"), tr("Examples:"), tr("Available Parameters:"), parameterHelp,
		detailTip,
		tr("CLI Control Flags:"),
		localizedFixedFlagsHelp())
}
