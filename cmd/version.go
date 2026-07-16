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

var clientVersion = "1.0.50"

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
				strings.EqualFold(strings.TrimSpace(getenv("AI_AGENT")), "TRAE") ||
				strings.EqualFold(strings.TrimSpace(getenv("ICUBE_PRODUCT_BRAND_NAME")), "TRAE")
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
	// agent is a generic fallback placed last: any specific invoker above
	// takes priority, and it only matches when a caller advertises itself as
	// an agent via the generic AGENT/IS_AGENT environment variables.
	{
		name: "agent",
		match: func(getenv envGetter) bool {
			return hasEnv(getenv, "AGENT") ||
				hasEnv(getenv, "IS_AGENT")
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

	// Stop probing as soon as one invoker matches: a single caller drives the
	// CLI, so the first hit (specific invokers first, generic agent last) wins.
	for _, detector := range skillInvokerDetectors {
		if detector.match(getenv) {
			return []string{detector.name}
		}
	}
	return nil
}

func hasEnv(getenv envGetter, key string) bool {
	return strings.TrimSpace(getenv(key)) != ""
}
