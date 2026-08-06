package cmd

// Copyright 2022 Beijing Volcanoengine Technology Ltd.  All Rights Reserved.

import (
	"fmt"
	"strings"
)

// allowedLegacyFixedFlags keeps triple-dash aliases working as an explicit
// system-flag escape when an action exposes the same double-dash parameter.
// The aliases are intentionally omitted from help and completion output.
var allowedLegacyFixedFlags = map[string]struct{}{
	"profile":  {},
	"region":   {},
	"endpoint": {},
}

var publicSystemFlags = map[string]struct{}{
	"profile":  {},
	"region":   {},
	"endpoint": {},
	"lang":     {},
}

const supportedSystemFlagsMessage = "--profile, --region, --endpoint, --lang"

func localizedSystemFlagsHelp() string {
	return `  --profile string    ` + tr("Use a configured profile only for this invocation.") + `
  --region string     ` + tr("Override the region only for this invocation.") + `
  --endpoint string   ` + tr("Override the endpoint only for this invocation.") + `
  --lang string       ` + tr("Set the display language for this invocation (EN or ZH).")
}

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
	if len(p.args)%2 == 0 && p.hasFlagSyntaxAtPairStarts() {
		return p.readPairedArgs(ctx)
	}

	return p.readLegacyArgs(ctx)
}

func (p *Parser) hasFlagSyntaxAtPairStarts() bool {
	for i := 0; i < len(p.args); i += 2 {
		if !strings.HasPrefix(p.args[i], "--") {
			return false
		}
	}
	return true
}

func (p *Parser) readPairedArgs(ctx *Context) ([]string, error) {
	for p.currentIndex < len(p.args) {
		name := p.args[p.currentIndex]
		value := p.args[p.currentIndex+1]
		p.currentIndex += 2

		flag, positional, err := p.parseArg(name, ctx)
		if err != nil {
			return nil, err
		}
		if flag == nil {
			return nil, fmt.Errorf("%q is not a valid flag", positional)
		}

		p.currentFlag = flag
		if value == "" {
			err = p.currentFlagValueError(ctx)
			p.currentFlag = nil
			return nil, err
		}
		flag.SetValue(value)
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

	if flag == nil { //解析普通参数
		if p.currentFlag != nil {
			if value == "" {
				err = p.currentFlagValueError(ctx)
			}
			p.currentFlag.SetValue(value)
			p.currentFlag = nil
		} else {
			arg = value
		}
	} else { //解析flag
		p.currentFlag = flag
	}
	return
}

func (p *Parser) currentFlagValueError(ctx *Context) error {
	prefix := p.currentFlag.prefix
	if prefix == "" {
		prefix = "--"
	}
	return fmt.Errorf("%s%s must set value. ", prefix, p.currentFlag.Name)
}

func (p *Parser) parseArg(arg string, ctx *Context) (flag *Flag, value string, err error) {
	if strings.HasPrefix(arg, "---") {
		// Triple-dash aliases force system-flag routing but are not advertised.
		name := arg[3:]
		if name == "" {
			err = fmt.Errorf("--- is not a valid flag")
			return
		}
		if _, ok := allowedLegacyFixedFlags[name]; !ok {
			err = fmt.Errorf("---%s is not supported, supported system flags: %s", name, supportedSystemFlagsMessage)
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
			_, isSystemFlag := publicSystemFlags[name]
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
