package cli

type Context struct {
	fixedFlags   *FlagSet
	dynamicFlags *FlagSet
	command      *Command
	config       *Configure
}

func NewContext() *Context {
	return &Context{
		fixedFlags:   NewFlagSet(),
		dynamicFlags: NewFlagSet(),
	}
}

func (c *Context) SetCommand(cmd *Command) {
	c.command = cmd
}

func (c *Context) SetConfig(cfg *Configure) {
	c.config = cfg
}
