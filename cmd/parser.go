package cmd

// Copyright 2022 Beijing Volcanoengine Technology Ltd.  All Rights Reserved.

import (
	"fmt"
	"strings"
)

// allowedFixedFlags 三横线（---）CLI 控制参数白名单，与双横线 API 参数区分。
// force 专用于跳过元数据校验；version/method 在正常与 force 路径均可覆盖元数据。新增固定参数时需同步更新 supportedFixedFlagsMessage 与文档。
var allowedFixedFlags = map[string]struct{}{
	"profile":  {},
	"region":   {},
	"endpoint": {},
	"force":    {},
	"version":  {},
	"method":   {},
}

// booleanFixedFlags 无需跟值的开关型固定参数（参考阿里云 CLI 的 AssignedNone）。
var booleanFixedFlags = map[string]struct{}{
	"force": {},
}

const supportedFixedFlagsMessage = "---profile, ---region, ---endpoint, ---force, ---version, ---method"

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
			// ---force 出现在参数末尾时，自动视为 true。
			if isBooleanFixedFlag(p.currentFlag.Name) {
				p.currentFlag.SetValue("true")
				p.currentFlag = nil
			} else {
				err = p.currentFlagValueError(ctx)
				p.currentFlag = nil
			}
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
			} else if isBooleanFixedFlag(p.currentFlag.Name) && !isBooleanLiteral(value) {
				// ---force DescribeAction：下一个 token 是 action 名，不能当作 force 的值。
				p.currentFlag.SetValue("true")
				p.currentFlag = nil
				arg = value
			} else {
				p.currentFlag.SetValue(value)
				p.currentFlag = nil
			}
		} else {
			arg = value
		}
	} else { //解析flag
		if isBooleanFixedFlag(flag.Name) {
			switch {
			case p.currentIndex >= len(p.args) || isFlagToken(p.args[p.currentIndex]):
				// ---force 位于末尾或后接其它 flag：开关置 true，不消费下一 token。
				flag.SetValue("true")
				p.currentFlag = nil
			case isBooleanLiteral(p.args[p.currentIndex]):
				// ---force false：显式布尔字面量由下一轮赋值。
				p.currentFlag = flag
			default:
				// ---force DescribeAction：后接 action 名，置 true 并保留下一 token 给位置参数。
				flag.SetValue("true")
				p.currentFlag = nil
			}
		} else {
			p.currentFlag = flag
		}
	}
	return
}

func isBooleanFixedFlag(name string) bool {
	_, ok := booleanFixedFlags[name]
	return ok
}

func isFlagToken(arg string) bool {
	return strings.HasPrefix(arg, "--") || strings.HasPrefix(arg, "---")
}

func isBooleanLiteral(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "true", "false", "1", "0", "yes", "no":
		return true
	default:
		return false
	}
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
