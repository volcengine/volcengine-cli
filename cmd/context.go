package cmd

// Copyright 2022 Beijing Volcanoengine Technology Ltd.  All Rights Reserved.

type Context struct {
	fixedFlags   *FlagSet
	dynamicFlags *FlagSet
	config       *Configure
	debugLogger  *DebugLogger
	// useStandardEndpointResolver 由 invocation 层在 force 且无显式 ---endpoint 时设置。
	useStandardEndpointResolver bool
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
