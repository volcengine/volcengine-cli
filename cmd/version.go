package cmd

import (
	"fmt"
	"os"
	"runtime"
	"strings"

	"github.com/volcengine/volcengine-go-sdk/volcengine/request"
)

var clientVersionAndUserAgentHandler = request.NamedHandler{
	Name: "VolcengineCliUserAgentHandler",
	Fn: func(r *request.Request) {
		request.AddToUserAgent(r, clientUserAgent(os.Getenv))
	},
}

const clientName = "volcengine-cli"
const clientVersion = "1.0.47"

type envGetter func(string) string

type skillInvokerDetector struct {
	name  string
	match func(envGetter) bool
}

var skillInvokerDetectors = []skillInvokerDetector{
	{
		name: "claude-code",
		match: func(getenv envGetter) bool {
			return hasEnv(getenv, "CLAUDECODE") ||
				hasEnv(getenv, "CLAUDE_CODE") ||
				hasEnv(getenv, "CLAUDE_CODE_CHILD_SESSION")
		},
	},
	{
		name: "trae",
		match: func(getenv envGetter) bool {
			return hasEnv(getenv, "TRAE_CLI_PLUGIN_ROOT") ||
				hasEnv(getenv, "COCO_PLUGIN_ROOT") ||
				strings.EqualFold(strings.TrimSpace(getenv("AI_AGENT")), "TRAE")
		},
	},
	{
		name: "cursor",
		match: func(getenv envGetter) bool {
			return hasEnv(getenv, "CURSOR_AGENT") ||
				hasEnv(getenv, "CURSOR_TRACE_ID") ||
				strings.TrimSpace(getenv("CURSOR_EXTENSION_HOST_ROLE")) == "agent-exec"
		},
	},
	{
		name: "codex",
		match: func(getenv envGetter) bool {
			return hasEnv(getenv, "CODEX_THREAD_ID") ||
				hasEnv(getenv, "CODEX_CI") ||
				hasEnv(getenv, "CODEX_SANDBOX")
		},
	},
	{
		name: "gemini-cli",
		match: func(getenv envGetter) bool {
			return hasEnv(getenv, "GEMINI_CLI")
		},
	},
	{
		name: "openclaw",
		match: func(getenv envGetter) bool {
			return hasEnv(getenv, "OPENCLAW_CLI") ||
				hasEnv(getenv, "OPENCLAW_SHELL")
		},
	},
	{
		name: "opencode",
		match: func(getenv envGetter) bool {
			return hasEnv(getenv, "OPENCODE")
		},
	},
}

func clientUserAgent(getenv envGetter) string {
	extra := []string{runtime.Version(), runtime.GOOS, runtime.GOARCH}
	if getenv != nil {
		for _, invoker := range detectSkillInvokers(getenv) {
			extra = append(extra, "skill-invoker/"+invoker)
		}
	}
	return fmt.Sprintf("%s/%s/(%s)", clientName, clientVersion, strings.Join(extra, "; "))
}

func detectSkillInvokers(getenv envGetter) []string {
	if getenv == nil {
		return nil
	}

	invokers := make([]string, 0, len(skillInvokerDetectors))
	for _, detector := range skillInvokerDetectors {
		if detector.match(getenv) {
			invokers = append(invokers, detector.name)
		}
	}
	return invokers
}

func hasEnv(getenv envGetter, key string) bool {
	return strings.TrimSpace(getenv(key)) != ""
}
