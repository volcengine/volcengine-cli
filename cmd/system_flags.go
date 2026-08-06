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
)

type systemFlagsResolution struct {
	args       []string
	fixedFlags map[string]string
}

// resolveSystemFlags extracts flags that must be applied before Cobra renders
// help or runs an action. API system flags must follow the action; other action
// flags stay in place for Parser to resolve against the action's metadata.
//
// Only preprocessableSystemFlags (profile/region/endpoint/lang) are stripped
// here. force/version/method remain in-place for Parser so root `ve --version`
// (CLI version) and presence-only --force keep working.
func resolveSystemFlags(args []string) (systemFlagsResolution, error) {
	result := systemFlagsResolution{
		args:       make([]string, 0, len(args)),
		fixedFlags: make(map[string]string),
	}
	state := beforeCommand
	serviceName := ""
	var actionParameters map[string]struct{}
	leadingSystemFlag := ""

	for i := 0; i < len(args); i++ {
		arg := args[i]
		name, legacy, equals, candidate := parseSystemFlagToken(arg)
		if candidate && state == beforeAPIAction {
			return result, systemFlagPositionError(name, legacy)
		}
		if candidate && state == beforeCommand && leadingSystemFlag == "" {
			leadingSystemFlag = systemFlagDisplayName(name, legacy)
		}
		if candidate && shouldExtractSystemFlag(name, legacy, state, actionParameters) {
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
			if i+1 < len(args) {
				result.args = append(result.args, args[i+1])
				i++
			}
			continue
		}

		if strings.HasPrefix(arg, "-") {
			if state == beforeAPIAction && isValueTakingFlag(arg, actionParameters) && i+1 < len(args) {
				result.args = append(result.args, args[i+1])
				i++
			}
			continue
		}

		switch state {
		case beforeCommand:
			serviceName = strings.ReplaceAll(arg, "_", "")
			if rootSupport.IsValidSvc(serviceName) {
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
	trimmed := ""
	if strings.HasPrefix(arg, "---") {
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
	if _, ok = publicSystemFlags[trimmed]; !ok {
		return "", false, false, false
	}
	return trimmed, legacy, equals, true
}

func shouldExtractSystemFlag(name string, legacy bool, state commandScanState, actionParameters map[string]struct{}) bool {
	if _, ok := preprocessableSystemFlags[name]; !ok {
		// force/version/method: position-checked as system flags but parsed later.
		return false
	}
	switch state {
	case beforeCommand:
		// Root help and non-API commands have no action. If a service is found
		// later, resolveSystemFlags rejects the leading system flag.
		return true
	case nonAPICommand:
		return name == "lang"
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
	// Root CLI version switch is not a value-taking flag. Action-scoped API
	// --version is value-taking and is handled after action via Parser.
	if arg == "--version" {
		return false
	}
	name, legacy := flagNameFromToken(arg)
	if name == "" {
		return strings.HasPrefix(arg, "--")
	}
	// Presence-only system force must not swallow the following token.
	if isPresenceOnlyFixedFlag(name) {
		if legacy {
			return false
		}
		if _, isActionParameter := actionParameters[name]; !isActionParameter {
			return false
		}
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
