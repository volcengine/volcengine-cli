package cmd

import (
	"strings"
)

// systemFlagDef is the single source of truth for CLI system flags.
// Parser routing, preprocess stripping, and completion registration derive
// from this table. Public help strings stay as tr() literals in
// localizedSystemFlagsHelp (i18n catalog requires constants) and are guarded
// by TestSystemFlagHelpMatchesDefs.
type systemFlagDef struct {
	name string
	// public marks the flag as a double-dash system flag (publicSystemFlags).
	public bool
	// legacyEscape marks ---name as a parser-accepted conflict escape
	// (allowedLegacyFixedFlags). lang is preprocess-only and has no parser legacy form.
	legacyEscape bool
	// preprocess marks flags that resolveSystemFlags may strip before cobra/parser
	// (profile/region/endpoint/lang). force/version/method stay in-place for Parser
	// so root `ve --version` and presence-only --force keep working.
	preprocess bool
	// presenceOnly: flag appears without consuming the next token when routed as system.
	presenceOnly bool
}

// systemFlagDefs order is the public help / supported-list order.
var systemFlagDefs = []systemFlagDef{
	{name: "profile", public: true, legacyEscape: true, preprocess: true},
	{name: "region", public: true, legacyEscape: true, preprocess: true},
	{name: "endpoint", public: true, legacyEscape: true, preprocess: true},
	{name: "lang", public: true, legacyEscape: false, preprocess: true}, // stripped only by resolveSystemFlags
	{name: "version", public: true, legacyEscape: true, preprocess: false},
	{name: "method", public: true, legacyEscape: true, preprocess: false},
	{name: "force", public: true, legacyEscape: true, preprocess: false, presenceOnly: true},
	{name: "output", public: true, legacyEscape: true, preprocess: false},
	{name: "query", public: true, legacyEscape: true, preprocess: false},
}

type systemFlagRegistry struct {
	public           map[string]struct{}
	legacyEscapes    map[string]struct{}
	preprocessable   map[string]struct{}
	presenceOnly     map[string]struct{}
	supportedMessage string
}

// systemFlags is initialized as one value so process argument preprocessing
// never observes partially initialized lookup maps during package startup.
var systemFlags = newSystemFlagRegistry(systemFlagDefs)

func newSystemFlagRegistry(defs []systemFlagDef) *systemFlagRegistry {
	registry := &systemFlagRegistry{
		public:         make(map[string]struct{}, len(defs)),
		legacyEscapes:  make(map[string]struct{}, len(defs)),
		preprocessable: make(map[string]struct{}, len(defs)),
		presenceOnly:   make(map[string]struct{}),
	}

	publicNames := make([]string, 0, len(defs))
	for _, d := range defs {
		if d.public {
			registry.public[d.name] = struct{}{}
			publicNames = append(publicNames, "--"+d.name)
		}
		if d.legacyEscape {
			registry.legacyEscapes[d.name] = struct{}{}
		}
		if d.preprocess {
			registry.preprocessable[d.name] = struct{}{}
		}
		if d.presenceOnly {
			registry.presenceOnly[d.name] = struct{}{}
		}
	}
	registry.supportedMessage = strings.Join(publicNames, ", ")
	return registry
}

func isPresenceOnlyFixedFlag(name string) bool {
	_, ok := systemFlags.presenceOnly[name]
	return ok
}

// isSystemPresenceOnlyToken reports whether arg is a presence-only system flag
// in the current action context (no value token should be consumed).
func isSystemPresenceOnlyToken(arg string, actionParameters map[string]struct{}) bool {
	name, legacy := flagNameFromToken(arg)
	if name == "" || !isPresenceOnlyFixedFlag(name) {
		return false
	}
	if legacy {
		return true
	}
	_, conflict := actionParameters[name]
	return !conflict
}

// publicSystemFlagNames returns public system flag bare names in help order.
func publicSystemFlagNames() []string {
	out := make([]string, 0, len(systemFlagDefs))
	for _, d := range systemFlagDefs {
		if d.public {
			out = append(out, d.name)
		}
	}
	return out
}

// localizedSystemFlagsHelp returns public double-dash system flag help plus
// reserved double-dash controls. Triple-dash aliases are intentionally omitted.
// Help strings are literal tr("...") constants so i18n catalog checks stay static.
func localizedSystemFlagsHelp() string {
	// Keep in lockstep with systemFlagDefs (name order and semantics).
	return `  --profile string     ` + tr("Use a configured profile only for this invocation.") + `
  --region string      ` + tr("Override the region only for this invocation.") + `
  --endpoint string    ` + tr("Override the endpoint only for this invocation.") + `
  --lang string        ` + tr("Set the display language for this invocation (EN or ZH).") + `
  --version string     ` + tr("API version; uses metadata when omitted (required with --force for unlisted services).") + `
  --method string      ` + tr("HTTP method GET or POST; explicit value overrides metadata, else metadata, else GET.") + `
  --force              ` + tr("Skip service/action metadata validation and force the call (presence-only; write --force alone, not --force true).") + `
  --output string      ` + tr("Set response output format (json|table|text|yaml|yaml-stream|off). Default: json.") + `
  --query string       ` + tr("JMESPath expression to filter/project the response before formatting.") + `

` + tr("Reserved double-dash controls (not API parameters):") + `
  --header string      ` + tr("Add a custom HTTP header as Name=Value; repeatable. Content-Type overrides metadata when set. Host/Authorization/Content-Length are blocked.") + `
  --body string        ` + tr("JSON request body for application/json style calls; mutually exclusive with other API parameters.")
}
