package cmd

// Copyright 2022 Beijing Volcanoengine Technology Ltd.  All Rights Reserved.

type Context struct {
	fixedFlags   *FlagSet
	dynamicFlags *FlagSet
	config       *Configure
}

func NewContext() *Context {
	return &Context{
		fixedFlags:   NewFlagSet(),
		dynamicFlags: NewFlagSet(),
	}
}

func (c *Context) SetConfig(cfg *Configure) {
	c.config = cfg
}
