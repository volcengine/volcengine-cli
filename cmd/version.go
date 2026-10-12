package cmd

import (
	"fmt"
	"net/url"
	"os"
	"runtime"
	"strings"
	"unicode"

	"github.com/volcengine/volcengine-go-sdk/volcengine/request"
)

const clientName = "volcengine-cli"

var clientVersion = "1.2.0"

type envGetter func(string) (string, bool)

type skillInvokerDetector struct {
	name  string
	match func(envGetter) bool
}

var skillInvokerDetectors = []skillInvokerDetector{
	{
		name: "doubao",
		match: func(getenv envGetter) bool {
			return hasEnv(getenv, "DOUBAO_OFFICE_APP_ID") ||
				hasEnv(getenv, "DOUBAO_OFFICE_AGENT_NAME") ||
				hasEnv(getenv, "AILY_SIGNATURE")
		},
	},
	{
		name: "github-copilot",
		match: func(getenv envGetter) bool {
			return strings.TrimSpace(envValue(getenv, "COPILOT_CLI")) == "1"
		},
	},
	{
		name: "cline",
		match: func(getenv envGetter) bool {
			return hasEnvKey(getenv, "CLINE_ACTIVE") ||
				hasEnvKey(getenv, "CLINE_CONNECTOR_CLI_LAUNCH")
		},
	},
	{
		name: "qoder",
		match: func(getenv envGetter) bool {
			return hasEnv(getenv, "QODER_CLI") ||
				hasEnv(getenv, "QODERWORK_SOURCE_CHAT_ID") ||
				hasEnv(getenv, "DINGTALK_DWS_AGENTCODE") ||
				hasEnv(getenv, "QODER_AGENT_SDK_VERSION")
		},
	},
	{
		name: "workbuddy",
		match: func(getenv envGetter) bool {
			return hasEnv(getenv, "CODEBUDDY_HOST") ||
				hasEnv(getenv, "WORKBUDDY_APPLICATION_NAME") ||
				hasEnv(getenv, "WORKBUDDY_APP_NAME") ||
				hasEnv(getenv, "WORKBUDDY_APP_VERSION")
		},
	},
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
				strings.EqualFold(strings.TrimSpace(envValue(getenv, "AI_AGENT")), "TRAE") ||
				strings.EqualFold(strings.TrimSpace(envValue(getenv, "ICUBE_PRODUCT_BRAND_NAME")), "TRAE")
		},
	},
	{
		name: "vecopilot",
		match: func(getenv envGetter) bool {
			return hasEnv(getenv, "VECOPILOT")
		},
	},
	{
		name: "cursor",
		match: func(getenv envGetter) bool {
			return hasEnv(getenv, "CURSOR_AGENT") ||
				hasEnv(getenv, "CURSOR_TRACE_ID") ||
				strings.TrimSpace(envValue(getenv, "CURSOR_EXTENSION_HOST_ROLE")) == "agent-exec"
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
	// Keep these integrations at the lowest priority among specific invokers.
	{
		name: "deepseek-harness",
		match: func(getenv envGetter) bool {
			return hasEnv(getenv, "DSH_SHELL") ||
				hasEnv(getenv, "DSH_SESSION_ID") ||
				hasEnv(getenv, "DSH_PTY_SESSION_ID")
		},
	},
	{
		name: "pi",
		match: func(getenv envGetter) bool {
			return hasEnv(getenv, "PI_CODING_AGENT") ||
				hasEnv(getenv, "PI_SESSION_ID") ||
				hasEnv(getenv, "PI_SESSION_FILE")
		},
	},
	{
		name: "hermes-agent",
		match: func(getenv envGetter) bool {
			return hasEnv(getenv, "HERMES_AGENT") ||
				hasEnv(getenv, "HERMES_DESKTOP")
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
	for _, invoker := range detectSkillInvokers(getenv) {
		// Escape caller-supplied names so they cannot change the metadata
		// separators or introduce invalid HTTP header characters.
		extra = append(extra, "skill-invoker/"+url.PathEscape(invoker))
		if skill := strings.TrimSpace(envValue(getenv, "VE_SKILL_NAME")); skill != "" {
			extra = append(extra, "skill/"+url.PathEscape(skill))
		}
	}
	return fmt.Sprintf("%s/%s/(%s)", clientName, clientVersion, strings.Join(extra, "; "))
}

func newClientVersionAndUserAgentHandler(customUserAgent string) request.NamedHandler {
	return request.NamedHandler{
		Name: "VolcengineCliUserAgentHandler",
		Fn: func(r *request.Request) {
			request.AddToUserAgent(r, clientUserAgent(os.LookupEnv))
			if customUserAgent != "" {
				request.AddToUserAgent(r, customUserAgent)
			}
		},
	}
}

func normalizeCustomUserAgent(value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", fmt.Errorf("--user-agent must set value")
	}
	for _, r := range value {
		if unicode.IsControl(r) {
			return "", fmt.Errorf("--user-agent must not contain control characters")
		}
	}
	return value, nil
}

func detectSkillInvokers(getenv envGetter) []string {
	if getenv == nil {
		return nil
	}

	// An explicit caller name always takes priority over automatic detection.
	if name := strings.TrimSpace(envValue(getenv, "VE_CALLER_NAME")); name != "" {
		return []string{name}
	}

	// Stop probing as soon as one invoker matches: a single caller drives the
	// CLI, so the first hit (specific invokers first, generic agent last) wins.
	for _, detector := range skillInvokerDetectors {
		if detector.match(getenv) {
			return []string{detector.name}
		}
	}
	if strings.EqualFold(strings.TrimSpace(envValue(getenv, "VE_CALLER_TYPE")), "ai_agent") {
		return []string{"agent"}
	}
	return nil
}

func hasEnv(getenv envGetter, key string) bool {
	return strings.TrimSpace(envValue(getenv, key)) != ""
}

// hasEnvKey distinguishes an explicitly exported empty value from an unset key.
func hasEnvKey(getenv envGetter, key string) bool {
	_, ok := getenv(key)
	return ok
}

func envValue(getenv envGetter, key string) string {
	value, _ := getenv(key)
	return value
}
