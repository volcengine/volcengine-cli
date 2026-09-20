package cmd

// Copyright 2022 Beijing Volcanoengine Technology Ltd.  All Rights Reserved.

type Context struct {
	fixedFlags   *FlagSet
	dynamicFlags *FlagSet
	config       *Configure
	debugLogger  *DebugLogger
	// invokedService preserves CLI spelling for safety rules; SDK routing uses
	// the canonical service name independently.
	invokedService string
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
