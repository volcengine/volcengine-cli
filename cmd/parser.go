package cmd

// Copyright 2022 Beijing Volcanoengine Technology Ltd.  All Rights Reserved.

import (
	"fmt"
	"strings"
)

// allowedLegacyFixedFlags keeps triple-dash aliases as an explicit system-flag
// escape when an action exposes the same double-dash parameter name.
// Aliases are intentionally omitted from help and completion output.
// 双横线保留控制参数（--header / --body）见 reservedDynamicFlags，不在此白名单。
// --lang / ---lang 由 resolveSystemFlags 预处理剥离，不进入 parser 白名单。
// 新增系统参数时需同步更新 publicSystemFlags、supportedSystemFlagsMessage、
// localizedSystemFlagsHelp、upgrade.rootValueFlags（若为带值 flag）与文档。
var allowedLegacyFixedFlags = map[string]struct{}{
	"profile":  {},
	"region":   {},
	"endpoint": {},
	"force":    {},
	"version":  {},
	"method":   {},
}

// publicSystemFlags are double-dash CLI system flags that route into fixedFlags
// when the action does not expose an exact-name API parameter conflict.
var publicSystemFlags = map[string]struct{}{
	"profile":  {},
	"region":   {},
	"endpoint": {},
	"lang":     {},
	"force":    {},
	"version":  {},
	"method":   {},
}

// preprocessableSystemFlags are stripped/applied by resolveSystemFlags before
// cobra/parser. force/version/method stay in-place for Parser (action-scoped).
var preprocessableSystemFlags = map[string]struct{}{
	"profile":  {},
	"region":   {},
	"endpoint": {},
	"lang":     {},
}

// booleanFixedFlags 纯开关型固定参数：出现即生效，不消费后续 token。
// 对双横线：仅在不与 action 参数冲突、作为系统参数解析时生效。
// 对三横线：始终作为系统开关（冲突逃逸）。
var booleanFixedFlags = map[string]struct{}{
	"force": {},
}

// supportedSystemFlagsMessage lists public double-dash system flags for docs/errors.
const supportedSystemFlagsMessage = "--profile, --region, --endpoint, --lang, --force, --version, --method"

// localizedSystemFlagsHelp 返回已本地化的 CLI 系统参数说明。
// 对外文档与 help 只展示双横线 system flags；三横线别名可用但不宣传。
// root/service/action usage 与未知 service help 共用，避免英文常量与 tr() 模板双源漂移。
func localizedSystemFlagsHelp() string {
	return `  --profile string     ` + tr("Use a configured profile only for this invocation.") + `
  --region string      ` + tr("Override the region only for this invocation.") + `
  --endpoint string    ` + tr("Override the endpoint only for this invocation.") + `
  --lang string        ` + tr("Set the display language for this invocation (EN or ZH).") + `
  --version string     ` + tr("API version; uses metadata when omitted (required with --force for unlisted services).") + `
  --method string      ` + tr("HTTP method GET or POST; explicit value overrides metadata, else metadata, else GET.") + `
  --force              ` + tr("Skip service/action metadata validation and force the call (presence-only; write --force alone, not --force true).") + `

` + tr("Reserved double-dash controls (not API parameters):") + `
  --header string      ` + tr("Add a custom HTTP header as Name=Value; repeatable. Content-Type overrides metadata when set. Host/Authorization/Content-Length are blocked.") + `
  --body string        ` + tr("JSON request body for application/json style calls; mutually exclusive with other API parameters.")
}

// localizedFixedFlagsHelp is kept as an alias for call sites that still use the old name.
func localizedFixedFlagsHelp() string {
	return localizedSystemFlagsHelp()
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
		a := p.args[i]
		if !strings.HasPrefix(a, "--") {
			return false
		}
		// Presence-only system --force / ---force must not pair-consume the next token.
		name, legacy := flagNameFromToken(a)
		if name == "" {
			return false
		}
		if isPresenceOnlyFixedFlag(name) {
			if legacy {
				return false
			}
			if _, isActionParameter := p.actionParameters[name]; !isActionParameter {
				return false
			}
		}
	}
	return true
}

func flagNameFromToken(arg string) (name string, legacy bool) {
	if strings.HasPrefix(arg, "---") {
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
		value := p.args[p.currentIndex+1]
		p.currentIndex += 2

		flag, positional, err := p.parseArg(name, ctx)
		if err != nil {
			return nil, err
		}
		if flag == nil {
			return nil, fmt.Errorf("%q is not a valid flag", positional)
		}

		// Explicit empty string is a valid API value (shell: --Name "").
		// Missing values are not represented as "" here; they fail earlier as unpaired flags.
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

	if flag == nil { //解析普通参数（含显式空字符串 ""，与「未提供 value」区分）
		if p.currentFlag != nil {
			// 空字符串是合法参数值；缺失 value 由「连续 flag」或 ReadArgs 收尾检查报错。
			p.currentFlag.SetValue(value)
			p.currentFlag = nil
		} else {
			arg = value
		}
	} else { //解析flag
		// presence-only 语义适用于作为系统参数的 force（--force 无冲突或 ---force）。
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

func isPresenceOnlyFixedFlag(name string) bool {
	_, ok := booleanFixedFlags[name]
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
	if strings.HasPrefix(arg, "---") {
		// Triple-dash aliases force system-flag routing (conflict escape).
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
