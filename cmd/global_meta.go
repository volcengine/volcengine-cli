package cmd

// global meta
var (
	rootSupport = NewRootSupport()
	ctx         *Context
	config      *Configure
)

func init() {
	ctx = NewContext()
	setRuntimeConfigTransaction(loadConfigTransaction())
}
