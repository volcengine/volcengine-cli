package cmd

import (
	"runtime"

	"github.com/volcengine/volcengine-go-sdk/volcengine/request"
)

var clientVersionAndUserAgentHandler = request.NamedHandler{
	Name: "VolcengineCliUserAgentHandler",
	Fn:   request.MakeAddToUserAgentHandler(clientName, clientVersion, runtime.Version(), runtime.GOOS, runtime.GOARCH),
}

const clientName = "volcengine-cli"
const clientVersion = "1.0.4"
