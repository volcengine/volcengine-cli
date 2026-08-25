package cmd

// global meta
var (
	rootSupport = NewRootSupport()
	ctx         *Context
	config      *Configure
)

func init() {
	config = LoadConfig()
	ctx = NewContext()
	ctx.SetConfig(config)
}
