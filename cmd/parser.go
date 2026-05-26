package cmd

// Copyright 2022 Beijing Volcanoengine Technology Ltd.  All Rights Reserved.

import (
	"fmt"
	"strings"
)

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
