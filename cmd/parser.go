package cmd

// Copyright 2022 Beijing Volcanoengine Technology Ltd.  All Rights Reserved.

import (
	"fmt"
	"strings"
)

// System-flag lookup sets are derived from systemFlagDefs in system_flag_defs.go.

type Parser struct {
	currentIndex     int
	args             []string
	currentFlag      *Flag
	actionParameters map[string]struct{}
}

func NewParser(args []string, actionParameters ...map[string]struct{}) *Parser {
	p := &Parser{
		args:             args,
		currentIndex:     0,
		currentFlag:      nil,
		actionParameters: map[string]struct{}{},
	}
	if len(actionParameters) > 0 && actionParameters[0] != nil {
		p.actionParameters = actionParameters[0]
	}
	return p
}

// ReadArgs parses invocation arguments with a single left-to-right pass, so
// the result never depends on how many arguments are given or where
// presence-only switches appear.
//
// A flag token opens a value slot (presence-only system switches excepted).
// The next token fills the slot verbatim unless it unambiguously starts another
// flag, in which case the open flag is reported as missing its value. Tokens
// outside a value slot that are not flags are returned as positional arguments.
func (p *Parser) ReadArgs(ctx *Context) ([]string, error) {
	if ctx == nil || ctx.fixedFlags == nil || ctx.dynamicFlags == nil {
		return nil, fmt.Errorf("invalid context for parsing arguments")
	}
	var positional []string
	for p.currentIndex < len(p.args) {
		arg := p.args[p.currentIndex]
		p.currentIndex++

		if p.currentFlag != nil {
			if p.startsFlag(arg) {
				return positional, p.currentFlagValueError(ctx)
			}
			// Values are opaque: empty strings, PEM text and flag-like literals
			// such as "--value", "--A=1" or "--yes" are stored unchanged.
			p.currentFlag.SetValue(arg)
			p.currentFlag = nil
			continue
		}

		flag, value, err := p.parseArg(arg, ctx)
		if err != nil {
			return positional, err
		}
		if flag == nil {
			if value != "" {
				positional = append(positional, value)
			}
			continue
		}
		if ctx.fixedFlags.GetByName(flag.Name) == flag && isPresenceOnlyFixedFlag(flag.Name) {
			// Presence-only switches are enabled by appearing and take no value.
			flag.SetValue("true")
			continue
		}
		p.currentFlag = flag
	}
	if p.currentFlag != nil {
		err := p.currentFlagValueError(ctx)
		p.currentFlag = nil
		return positional, err
	}
	if flag := ctx.fixedFlags.GetByName(customUserAgentFlagName); flag != nil {
		value, normalizeErr := normalizeCustomUserAgent(flag.GetValue())
		if normalizeErr != nil {
			return positional, normalizeErr
		}
		flag.SetValue(value)
	}
	return positional, nil
}

func flagNameFromToken(arg string) (name string, legacy bool) {
	if arg == "-y" {
		return "yes", false
	} else if strings.HasPrefix(arg, "---") {
		name = arg[3:]
		legacy = true
	} else if strings.HasPrefix(arg, "--") {
		name = arg[2:]
	} else {
		return "", false
	}
	if j := strings.IndexByte(name, '='); j >= 0 {
		name = name[:j]
	}
	return name, legacy
}

// startsFlag reports whether a token in value position must be read as the
// next flag instead of a literal value. Only names this invocation actually
// recognises qualify: API parameters from the action metadata, reserved CLI
// controls (--body, --header), value-taking system flags, and explicit
// ---system escapes. Presence-only switches and unknown names stay literal,
// so values such as "--yes" or "--value" survive.
func (p *Parser) startsFlag(arg string) bool {
	if strings.HasPrefix(arg, "---") {
		_, ok := systemFlags.legacyEscapes[arg[3:]]
		return ok
	}
	if !strings.HasPrefix(arg, "--") {
		return false
	}
	name := arg[2:]
	if name == "" || strings.Contains(name, "=") {
		return false
	}
	if p.isActionParameterName(name) {
		return true
	}
	if _, ok := reservedDynamicFlags[name]; ok {
		return true
	}
	if _, ok := systemFlags.public[name]; ok {
		return !isPresenceOnlyFixedFlag(name)
	}
	return false
}

// isActionParameterName matches name against the action's parameter names.
// Metadata spells array indices as "N" (Tags.N.Key) and JSON actions list only
// top-level fields, so indexed and nested spellings are matched as well.
func (p *Parser) isActionParameterName(name string) bool {
	if len(p.actionParameters) == 0 {
		return false
	}
	if _, ok := p.actionParameters[name]; ok {
		return true
	}
	segs := strings.Split(name, ".")
	if len(segs) == 1 {
		return false
	}
	if _, ok := p.actionParameters[segs[0]]; ok {
		return true
	}
	for i, seg := range segs {
		if isNumericSeg(seg) {
			segs[i] = "N"
		}
	}
	_, ok := p.actionParameters[strings.Join(segs, ".")]
	return ok
}

func (p *Parser) currentFlagValueError(ctx *Context) error {
	prefix := "--"
	if p.currentFlag != nil && p.currentFlag.prefix != "" {
		prefix = p.currentFlag.prefix
	} else if ctx != nil && p.currentFlag != nil && ctx.fixedFlags != nil && ctx.fixedFlags.GetByName(p.currentFlag.Name) == p.currentFlag {
		prefix = "---"
	}
	return fmt.Errorf("%s%s must set value. ", prefix, p.currentFlag.Name)
}

func (p *Parser) parseArg(arg string, ctx *Context) (flag *Flag, value string, err error) {
	if arg == "-y" {
		flag, err = ctx.fixedFlags.AddByName("yes")
		if flag != nil {
			flag.prefix = "-y"
		}
	} else if strings.HasPrefix(arg, "---") {
		// Triple-dash aliases force system-flag routing (conflict escape).
		name := arg[3:]
		if name == "" {
			err = fmt.Errorf("--- is not a valid flag")
			return
		}
		if _, ok := systemFlags.legacyEscapes[name]; !ok {
			err = fmt.Errorf("---%s is not supported, supported system flags: %s", name, systemFlags.supportedMessage)
			return
		}
		flag, err = ctx.fixedFlags.AddByName(name)
		if flag != nil {
			flag.prefix = "---"
		}
	} else if strings.HasPrefix(arg, "--") {
		if len(arg) == 2 {
			err = fmt.Errorf("-- is not support command")
		} else {
			name := arg[2:]
			if j := strings.IndexByte(name, '='); j >= 0 {
				err = fmt.Errorf("--%s does not support '=' syntax; use '--%s <value>'", name[:j], name[:j])
				return
			}
			_, isSystemFlag := systemFlags.public[name]
			_, isActionParameter := p.actionParameters[name]
			if isSystemFlag && !isActionParameter {
				flag, err = ctx.fixedFlags.AddByName(name)
			} else {
				flag, err = ctx.dynamicFlags.AddByName(name)
			}
			if flag != nil {
				flag.prefix = "--"
			}
		}
	} else {
		value = arg
	}
	return
}
