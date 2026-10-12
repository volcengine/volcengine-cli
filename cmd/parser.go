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

func (p *Parser) ReadArgs(ctx *Context) ([]string, error) {
	if ctx == nil || ctx.fixedFlags == nil || ctx.dynamicFlags == nil {
		return nil, fmt.Errorf("invalid context for parsing arguments")
	}
	var (
		args []string
		err  error
	)
	if p.hasPairedFlagSyntax() {
		args, err = p.readPairedArgs(ctx)
	} else {
		args, err = p.readLegacyArgs(ctx)
	}
	if err != nil {
		return args, err
	}
	if flag := ctx.fixedFlags.GetByName(customUserAgentFlagName); flag != nil {
		value, normalizeErr := normalizeCustomUserAgent(flag.GetValue())
		if normalizeErr != nil {
			return args, normalizeErr
		}
		flag.SetValue(value)
	}
	return args, nil
}

// hasPairedFlagSyntax recognizes flag/value pairs interleaved with system
// switches. Values remain opaque, including PEM text and flag-like strings.
func (p *Parser) hasPairedFlagSyntax() bool {
	for i := 0; i < len(p.args); {
		a := p.args[i]
		if !strings.HasPrefix(a, "--") && a != "-y" {
			return false
		}
		name, _ := flagNameFromToken(a)
		if name == "" {
			return false
		}
		if isSystemPresenceOnlyToken(a, p.actionParameters) {
			i++
			continue
		}
		if i+1 >= len(p.args) {
			return false
		}
		i += 2
	}
	return true
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

func (p *Parser) readPairedArgs(ctx *Context) ([]string, error) {
	for p.currentIndex < len(p.args) {
		name := p.args[p.currentIndex]
		p.currentIndex++

		flag, positional, err := p.parseArg(name, ctx)
		if err != nil {
			return nil, err
		}
		if flag == nil {
			return nil, fmt.Errorf("%q is not a valid flag", positional)
		}
		if ctx.fixedFlags.GetByName(flag.Name) == flag && isPresenceOnlyFixedFlag(flag.Name) {
			flag.SetValue("true")
			continue
		}

		// Explicit empty string is a valid API value (shell: --Name "").
		// hasPairedFlagSyntax has already checked that this value exists.
		flag.SetValue(p.args[p.currentIndex])
		p.currentIndex++
		p.currentFlag = nil
	}
	return nil, nil
}

func (p *Parser) readLegacyArgs(ctx *Context) ([]string, error) {
	var r []string
	for {
		arg, _, more, err := p.readArg(ctx)
		if err != nil {
			return r, err
		}
		if arg != "" {
			r = append(r, arg)
		}
		if !more {
			return r, nil
		}
	}
}

func (p *Parser) readArg(ctx *Context) (arg string, flag *Flag, more bool, err error) {
	//跳出条件
	if len(p.args) <= p.currentIndex {
		if p.currentFlag != nil {
			err = p.currentFlagValueError(ctx)
			p.currentFlag = nil
		}
		more = false
		return
	}
	//设置下一跳
	more = true
	//获取当前位置的入参
	_arg := p.args[p.currentIndex]
	p.currentIndex++
	//计算是参数还是flag
	var (
		value string
	)
	flag, value, err = p.parseArg(_arg, ctx)
	if err != nil {
		return
	}

	//不允许两个连续的空--
	if p.currentFlag != nil && flag != nil {
		err = p.currentFlagValueError(ctx)
	}

	if flag == nil { //解析普通参数（含显式空字符串 ""，与「未提供 value」区分）
		if p.currentFlag != nil {
			// 空字符串是合法参数值；缺失 value 由「连续 flag」或 ReadArgs 收尾检查报错。
			p.currentFlag.SetValue(value)
			p.currentFlag = nil
		} else {
			arg = value
		}
	} else { //解析flag
		// presence-only 语义适用于 force / yes 等纯开关系统参数。
		isFixedFlag := ctx != nil && ctx.fixedFlags != nil && ctx.fixedFlags.GetByName(flag.Name) == flag
		if isFixedFlag && isPresenceOnlyFixedFlag(flag.Name) {
			// 纯开关固定参数：出现即启用，不消费后续 token。
			flag.SetValue("true")
			p.currentFlag = nil
		} else {
			p.currentFlag = flag
		}
	}
	return
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
			if strings.HasPrefix(name, customUserAgentFlagName+"=") {
				err = fmt.Errorf("--user-agent does not support '=' syntax; use '--user-agent <value>'")
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
