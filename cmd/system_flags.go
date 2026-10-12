package cmd

import (
	"fmt"
	"strings"
)

type commandScanState int

const (
	beforeCommand commandScanState = iota
	beforeAPIAction
	afterAPIAction
	nonAPICommand
	proxyCLICommand
)

type systemFlagsResolution struct {
	args       []string
	fixedFlags map[string]string
}

// resolveSystemFlags extracts flags that must be applied before Cobra renders
// help or runs an action. API system flags must follow the action; other action
// flags stay in place for Parser to resolve against the action's metadata.
//
// Only registry preprocessable flags (profile/region/endpoint/lang) are stripped
// here. force/yes/version/method remain in-place for Parser so root `ve --version`
// (CLI version) and presence-only switches keep working.
func resolveSystemFlags(args []string) (systemFlagsResolution, error) {
	return resolveSystemFlagsWithRegistry(args, systemFlags)
}

func resolveSystemFlagsWithRegistry(args []string, registry *systemFlagRegistry) (systemFlagsResolution, error) {
	result := systemFlagsResolution{
		args:       make([]string, 0, len(args)),
		fixedFlags: make(map[string]string),
	}
	if registry == nil {
		return result, fmt.Errorf("system flag registry is not initialized")
	}
	state := beforeCommand
	serviceName := ""
	var actionParameters map[string]struct{}
	leadingSystemFlag := ""
	proxyManagement := false
	proxyRun := false

	for i := 0; i < len(args); i++ {
		arg := args[i]
		// Stop scanning entirely once the child owns argv (including empty values).
		if state == proxyCLICommand || (proxyRun && arg == "--") {
			result.args = append(result.args, args[i:]...)
			break
		}
		if proxyManagement && state == nonAPICommand && arg == "run" {
			proxyRun = true
		}
		name, legacy, equals, candidate := parseSystemFlagTokenWithRegistry(arg, registry)
		if candidate && state == beforeAPIAction {
			return result, systemFlagPositionError(name, legacy)
		}
		if candidate && state == beforeCommand && leadingSystemFlag == "" {
			leadingSystemFlag = systemFlagDisplayName(name, legacy)
		}
		if candidate && shouldExtractSystemFlagWithRegistry(name, legacy, state, actionParameters, registry) {
			if equals {
				return result, fmt.Errorf("%s does not support '=' syntax; use '--%s <value>'", arg, name)
			}
			if i+1 >= len(args) || strings.HasPrefix(args[i+1], "--") ||
				(name == "lang" && strings.HasPrefix(args[i+1], "-")) {
				return result, fmt.Errorf("--%s requires a value", name)
			}
			if _, exists := result.fixedFlags[name]; exists {
				return result, fmt.Errorf("--%s cannot be specified more than once", name)
			}
			result.fixedFlags[name] = args[i+1]
			i++
			continue
		}

		result.args = append(result.args, arg)
		if state == afterAPIAction && isValueTakingFlag(arg, actionParameters) {
			// Pair flag+value only when the next token is not another flag.
			// Otherwise `--detail --lang ZH` would swallow `--lang` and drop language.
			if i+1 < len(args) && !isCLIFlagToken(args[i+1]) {
				result.args = append(result.args, args[i+1])
				i++
			}
			continue
		}

		if strings.HasPrefix(arg, "-") {
			if state == beforeAPIAction && isValueTakingFlag(arg, actionParameters) && i+1 < len(args) && !isCLIFlagToken(args[i+1]) {
				result.args = append(result.args, args[i+1])
				i++
			}
			continue
		}

		switch state {
		case beforeCommand:
			serviceName = strings.ReplaceAll(arg, "_", "")
			proxyManagement = arg == "proxycli"
			if isProxyCLIInvocation(arg, args[i+1:]) || arg == "__complete" || arg == "__completeNoDesc" {
				// A proxy command owns every token after its catalog-provided second-level
				// name. In particular, do not consume arkcli's --lang/--profile.
				state = proxyCLICommand
			} else if rootSupport.IsValidSvc(serviceName) {
				if leadingSystemFlag != "" {
					return result, fmt.Errorf("%s must be specified after action", leadingSystemFlag)
				}
				state = beforeAPIAction
			} else {
				state = nonAPICommand
			}
		case beforeAPIAction:
			actionParameters = publicActionParameterNames(serviceName, arg)
			state = afterAPIAction
		}
	}

	return result, nil
}

func parseSystemFlagToken(arg string) (name string, legacy, equals, ok bool) {
	return parseSystemFlagTokenWithRegistry(arg, systemFlags)
}

func parseSystemFlagTokenWithRegistry(arg string, registry *systemFlagRegistry) (name string, legacy, equals, ok bool) {
	trimmed := ""
	if arg == "-y" {
		trimmed = "yes"
	} else if strings.HasPrefix(arg, "---") {
		trimmed = arg[3:]
		legacy = true
	} else if strings.HasPrefix(arg, "--") {
		trimmed = arg[2:]
	} else {
		return "", false, false, false
	}
	if index := strings.IndexByte(trimmed, '='); index >= 0 {
		trimmed = trimmed[:index]
		equals = true
	}
	if registry == nil {
		return "", false, false, false
	}
	if _, ok = registry.public[trimmed]; !ok {
		return "", false, false, false
	}
	return trimmed, legacy, equals, true
}

func shouldExtractSystemFlag(name string, legacy bool, state commandScanState, actionParameters map[string]struct{}) bool {
	return shouldExtractSystemFlagWithRegistry(name, legacy, state, actionParameters, systemFlags)
}

func shouldExtractSystemFlagWithRegistry(name string, legacy bool, state commandScanState, actionParameters map[string]struct{}, registry *systemFlagRegistry) bool {
	if registry == nil {
		return false
	}
	if _, ok := registry.preprocessable[name]; !ok {
		// force/yes/version/method: position-checked as system flags but parsed later.
		return false
	}
	switch state {
	case beforeCommand:
		// Root help and non-API commands have no action. If a service is found
		// later, resolveSystemFlags rejects the leading system flag.
		return true
	case nonAPICommand:
		return name == "lang"
	case proxyCLICommand:
		return false
	case afterAPIAction:
		if name != "lang" {
			return false
		}
		if legacy {
			return true
		}
		_, conflict := actionParameters[name]
		return !conflict
	default:
		return false
	}
}

func systemFlagPositionError(name string, legacy bool) error {
	return fmt.Errorf("%s must be specified after action", systemFlagDisplayName(name, legacy))
}

func systemFlagDisplayName(name string, legacy bool) string {
	if legacy {
		return "---" + name
	}
	return "--" + name
}

func isValueTakingFlag(arg string, actionParameters map[string]struct{}) bool {
	if arg == "-h" || arg == "--help" || arg == "-v" {
		return false
	}
	// Root CLI version switch is not a value-taking flag when scanned before a
	// service positional. Action-scoped API --version is still left in-place for
	// Parser (preprocess=false); not consuming its value here only splits tokens
	// and does not change parse results.
	if arg == "--version" {
		return false
	}
	// Help-mode presence-only control paired with -h/--help. Lowercase --detail is
	// reserved as a CLI help switch (API parameter Detail uses matching case).
	// Must not consume the following token, or `--detail --lang ZH` loses language.
	if arg == "--detail" {
		return false
	}
	if isSystemPresenceOnlyToken(arg, actionParameters) {
		return false
	}
	return strings.HasPrefix(arg, "--")
}

func applyResolvedSystemFlags(ctx *Context, values map[string]string) error {
	if ctx == nil || ctx.fixedFlags == nil {
		return fmt.Errorf("invalid context for resolved system flags")
	}
	for name, value := range values {
		flag, err := ctx.fixedFlags.AddByName(name)
		if err != nil {
			return err
		}
		flag.SetValue(value)
	}
	return nil
}
