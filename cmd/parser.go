package cmd

// Copyright 2022 Beijing Volcanoengine Technology Ltd.  All Rights Reserved.

import (
	"fmt"
	"strings"
)

// allowedFixedFlags 三横线（---）CLI 控制参数白名单，与双横线 API 参数区分。
// profile/region/endpoint 为通用运行时覆盖；force 跳过元数据校验；version/method
// 在正常与 force 路径均可覆盖元数据。
// 双横线保留控制参数（--header / --body）见 reservedDynamicFlags，不在此白名单。
// ---lang 由 resolveLanguage 预处理剥离，不进入本白名单。
// 新增 --- 固定参数时需同步更新 supportedFixedFlagsMessage、localizedFixedFlagsHelp、
// upgrade.rootValueFlags（若为带值 flag）与文档。
var allowedFixedFlags = map[string]struct{}{
	"profile":  {},
	"region":   {},
	"endpoint": {},
	"force":    {},
	"version":  {},
	"method":   {},
}

// booleanFixedFlags 纯开关型固定参数：出现即生效，不消费后续 token。
var booleanFixedFlags = map[string]struct{}{
	"force": {},
}

// supportedFixedFlagsMessage 是 parser 拒绝未知 ---flag 时展示的白名单列表。
// 不含 ---lang：语言参数在进入 parser 前已由 resolveLanguage 处理。
const supportedFixedFlagsMessage = "---profile, ---region, ---endpoint, ---force, ---version, ---method"

// localizedFixedFlagsHelp 返回已本地化的 CLI 控制参数说明。
// 分两块：三横线 fixed flags，以及双横线保留控制参数（--header / --body）。
// root/service/action usage 与未知 service help 共用，避免英文常量与 tr() 模板双源漂移。
func localizedFixedFlagsHelp() string {
	return `  ---profile string    ` + tr("Use a configured profile only for this invocation.") + `
  ---region string     ` + tr("Override the region only for this invocation.") + `
  ---endpoint string   ` + tr("Override the endpoint only for this invocation.") + `
  ---version string    ` + tr("API version; uses metadata when omitted (required with ---force for unlisted services).") + `
  ---method string     ` + tr("HTTP method GET or POST; explicit value overrides metadata, else metadata, else GET.") + `
  ---force             ` + tr("Skip service/action metadata validation and force the call (presence-only; write ---force alone, not ---force true).") + `
  ---lang string       ` + tr("Set the display language for this invocation (EN or ZH).") + `

` + tr("Reserved double-dash controls (not API parameters):") + `
  --header string      ` + tr("Add a custom HTTP header as Name=Value; repeatable. Content-Type overrides metadata when set. Host/Authorization/Content-Length are blocked.") + `
  --body string        ` + tr("JSON request body for application/json style calls; mutually exclusive with other API parameters.")
}

type Parser struct {
	currentIndex int
	args         []string
	currentFlag  *Flag
}

func NewParser(args []string) *Parser {
	return &Parser{
		args:         args,
		currentIndex: 0,
		currentFlag:  nil,
	}
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
			} else {
				p.currentFlag.SetValue(value)
				p.currentFlag = nil
			}
		} else {
			arg = value
		}
	} else { //解析flag
		// presence-only 语义仅适用于三横线 fixed flag；双横线 dynamic flag 仍需显式 value。
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
	if ctx != nil && ctx.fixedFlags != nil && ctx.fixedFlags.GetByName(p.currentFlag.Name) == p.currentFlag {
		prefix = "---"
	}
	return fmt.Errorf("%s%s must set value. ", prefix, p.currentFlag.Name)
}

func (p *Parser) parseArg(arg string, ctx *Context) (flag *Flag, value string, err error) {
	if strings.HasPrefix(arg, "---") {
		// CLI 内部 flag（如 ---profile, ---region），存入 fixedFlags
		name := arg[3:]
		if name == "" {
			err = fmt.Errorf("--- is not a valid flag")
			return
		}
		if _, ok := allowedFixedFlags[name]; !ok {
			err = fmt.Errorf("---%s is not supported, supported fixed flags: %s", name, supportedFixedFlagsMessage)
			return
		}
		flag, err = ctx.fixedFlags.AddByName(name)
	} else if strings.HasPrefix(arg, "--") {
		if len(arg) == 2 {
			err = fmt.Errorf("-- is not support command")
		} else {
			//可变参数放入动态参数集合中
			flag, err = ctx.dynamicFlags.AddByName(arg[2:])
		}
	} else {
		value = arg
	}
	return
}
