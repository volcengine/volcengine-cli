package cmd

// global meta
var (
	rootSupport = NewRootSupport()
	ctx         *Context
	config      *Configure
)

func init() {
	if !localOnlyInvocation(processLanguageResolution.args) {
		var err error
		config, err = loadConfig()
		warnConfigLoadError(err)
	}
	ctx = NewContext()
	ctx.SetConfig(config)
}
