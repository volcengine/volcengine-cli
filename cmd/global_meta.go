package cmd

// global meta
var (
	rootSupport = NewRootSupport()
	ctx         *Context
	config      *Configure
)

func init() {
	if !localOnlyInvocation(processLanguageResolution.args) {
		config = LoadConfig()
	}
	ctx = NewContext()
	ctx.SetConfig(config)
}
