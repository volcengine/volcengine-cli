package cmd

import (
	"net/http"
	"runtime"
	"strings"
	"testing"

	"github.com/volcengine/volcengine-go-sdk/volcengine/request"
)

func TestClientUserAgentDefault(t *testing.T) {
	got := clientUserAgent(testEnv(nil))
	want := clientName + "/" + clientVersion + "/(" + runtime.Version() + "; " + runtime.GOOS + "; " + runtime.GOARCH + ")"
	if got != want {
		t.Fatalf("clientUserAgent() = %q, want %q", got, want)
	}
}

func TestClientUserAgentDefaultWithNilEnvGetter(t *testing.T) {
	got := clientUserAgent(nil)
	want := clientName + "/" + clientVersion + "/(" + runtime.Version() + "; " + runtime.GOOS + "; " + runtime.GOARCH + ")"
	if got != want {
		t.Fatalf("clientUserAgent(nil) = %q, want %q", got, want)
	}
}

func TestDetectSkillInvokers(t *testing.T) {
	tests := []struct {
		name string
		env  map[string]string
		want []string
	}{
		{
			name: "claude code",
			env:  map[string]string{"CLAUDECODE": "1"},
			want: []string{"claude-code"},
		},
		{
			name: "trae plugin root",
			env:  map[string]string{"TRAE_CLI_PLUGIN_ROOT": "/tmp/trae-plugin"},
			want: []string{"trae"},
		},
		{
			name: "coco plugin root",
			env:  map[string]string{"COCO_PLUGIN_ROOT": "/tmp/coco-plugin"},
			want: []string{"trae"},
		},
		{
			name: "trae ide ai agent",
			env:  map[string]string{"AI_AGENT": "TRAE"},
			want: []string{"trae"},
		},
		{
			name: "trae ide ai agent is case insensitive",
			env:  map[string]string{"AI_AGENT": " trae "},
			want: []string{"trae"},
		},
		{
			name: "cursor cli",
			env:  map[string]string{"CURSOR_AGENT": "1"},
			want: []string{"cursor"},
		},
		{
			name: "cursor trace",
			env:  map[string]string{"CURSOR_TRACE_ID": "trace-id"},
			want: []string{"cursor"},
		},
		{
			name: "cursor ide agent exec",
			env:  map[string]string{"CURSOR_EXTENSION_HOST_ROLE": "agent-exec"},
			want: []string{"cursor"},
		},
		{
			name: "codex",
			env:  map[string]string{"CODEX_THREAD_ID": "thread-id"},
			want: []string{"codex"},
		},
		{
			name: "codex sandbox",
			env:  map[string]string{"CODEX_SANDBOX": "seatbelt"},
			want: []string{"codex"},
		},
		{
			name: "gemini cli",
			env:  map[string]string{"GEMINI_CLI": "1"},
			want: []string{"gemini-cli"},
		},
		{
			name: "openclaw",
			env:  map[string]string{"OPENCLAW_CLI": "1"},
			want: []string{"openclaw"},
		},
		{
			name: "openclaw shell",
			env:  map[string]string{"OPENCLAW_SHELL": "exec"},
			want: []string{"openclaw"},
		},
		{
			name: "opencode",
			env:  map[string]string{"OPENCODE": "1"},
			want: []string{"opencode"},
		},
		{
			name: "blank values are ignored",
			env: map[string]string{
				"AI_AGENT":                   " ",
				"CLAUDECODE":                 " ",
				"CLAUDE_CODE":                " ",
				"CLAUDE_CODE_CHILD_SESSION":  " ",
				"CODEX_THREAD_ID":            "",
				"CODEX_CI":                   " ",
				"CODEX_SANDBOX":              " ",
				"GEMINI_CLI":                 " ",
				"CURSOR_AGENT":               " ",
				"CURSOR_TRACE_ID":            " ",
				"CURSOR_EXTENSION_HOST_ROLE": " ",
				"OPENCODE_CLIENT":            "\t",
				"OPENCLAW_CLI":               "\n",
				"OPENCLAW_SHELL":             " ",
				"TRAECLI_SESSION_ID":         " ",
				"TRAE_CLI_PLUGIN_ROOT":       " ",
				"COCO_PLUGIN_ROOT":           " ",
				"OPENCODE":                   " ",
			},
			want: nil,
		},
		{
			name: "non skill invocation variables are ignored",
			env: map[string]string{
				"CODEX_HOME":             "/tmp/codex",
				"CURSOR_CONFIG_DIR":      "/tmp/cursor",
				"GEMINI_CLI_HOME":        "/tmp/gemini",
				"GEMINI_CLI_SURFACE":     "custom-surface",
				"GEMINI_API_KEY":         "api-key",
				"AI_AGENT":               "CODEX",
				"OPENCODE_CLIENT":        "cli",
				"PROCESS_LAUNCHED_BY_CW": "1",
				"PROCESS_LAUNCHED_BY_Q":  "1",
				"TRAE_CONFIG_FILE":       "/tmp/trae/config.json",
				"TRAECLI_SESSION_ID":     "session-id",
			},
			want: nil,
		},
		{
			name: "deterministic order",
			env: map[string]string{
				"CODEX_CI":    "1",
				"CLAUDE_CODE": "1",
				"GEMINI_CLI":  "1",
				"OPENCODE":    "1",
			},
			want: []string{"claude-code", "codex", "gemini-cli", "opencode"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := detectSkillInvokers(testEnv(tt.env))
			if strings.Join(got, ",") != strings.Join(tt.want, ",") {
				t.Fatalf("detectSkillInvokers() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestClientUserAgentIncludesSkillInvokerMetadata(t *testing.T) {
	got := clientUserAgent(testEnv(map[string]string{
		"CLAUDECODE":      "1",
		"CODEX_THREAD_ID": "thread-id",
	}))

	for _, want := range []string{
		clientName + "/" + clientVersion + "/(",
		runtime.Version(),
		runtime.GOOS,
		runtime.GOARCH,
		"skill-invoker/claude-code",
		"skill-invoker/codex",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("clientUserAgent() = %q, want it to contain %q", got, want)
		}
	}
}

func TestClientVersionAndUserAgentHandlerAddsSkillInvokerToHeader(t *testing.T) {
	tests := []struct {
		name string
		env  map[string]string
		want string
	}{
		{
			name: "claude code",
			env:  map[string]string{"CLAUDECODE": "1"},
			want: "skill-invoker/claude-code",
		},
		{
			name: "trae plugin root",
			env:  map[string]string{"TRAE_CLI_PLUGIN_ROOT": "/tmp/trae-plugin"},
			want: "skill-invoker/trae",
		},
		{
			name: "trae ide ai agent",
			env:  map[string]string{"AI_AGENT": "TRAE"},
			want: "skill-invoker/trae",
		},
		{
			name: "cursor",
			env:  map[string]string{"CURSOR_AGENT": "1"},
			want: "skill-invoker/cursor",
		},
		{
			name: "codex",
			env:  map[string]string{"CODEX_THREAD_ID": "thread-id"},
			want: "skill-invoker/codex",
		},
		{
			name: "gemini cli",
			env:  map[string]string{"GEMINI_CLI": "1"},
			want: "skill-invoker/gemini-cli",
		},
		{
			name: "openclaw",
			env:  map[string]string{"OPENCLAW_SHELL": "exec"},
			want: "skill-invoker/openclaw",
		},
		{
			name: "opencode",
			env:  map[string]string{"OPENCODE": "1"},
			want: "skill-invoker/opencode",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			clearSkillInvokerEnv(t)
			for key, value := range tt.env {
				t.Setenv(key, value)
			}

			httpRequest, err := http.NewRequest(http.MethodGet, "https://example.com", nil)
			if err != nil {
				t.Fatal(err)
			}
			httpRequest.Header.Set("User-Agent", "existing-client/0.1")
			req := &request.Request{HTTPRequest: httpRequest}

			clientVersionAndUserAgentHandler.Fn(req)
			got := req.HTTPRequest.Header.Get("User-Agent")
			for _, want := range []string{
				"existing-client/0.1 ",
				clientName + "/" + clientVersion + "/(",
				runtime.Version(),
				runtime.GOOS,
				runtime.GOARCH,
				tt.want,
			} {
				if !strings.Contains(got, want) {
					t.Fatalf("User-Agent = %q, want it to contain %q", got, want)
				}
			}

			for _, unexpected := range []string{
				"skill-invoker/claude-code",
				"skill-invoker/trae",
				"skill-invoker/cursor",
				"skill-invoker/codex",
				"skill-invoker/gemini-cli",
				"skill-invoker/openclaw",
				"skill-invoker/opencode",
			} {
				if unexpected != tt.want && strings.Contains(got, unexpected) {
					t.Fatalf("User-Agent = %q, did not expect it to contain %q", got, unexpected)
				}
			}
		})
	}
}

func testEnv(values map[string]string) envGetter {
	return func(key string) string {
		return values[key]
	}
}

func clearSkillInvokerEnv(t *testing.T) {
	for _, key := range []string{
		"AI_AGENT",
		"CLAUDECODE",
		"CLAUDE_CODE",
		"CLAUDE_CODE_CHILD_SESSION",
		"TRAECLI_SESSION_ID",
		"TRAE_CLI_PLUGIN_ROOT",
		"COCO_PLUGIN_ROOT",
		"CURSOR_AGENT",
		"CURSOR_TRACE_ID",
		"CURSOR_EXTENSION_HOST_ROLE",
		"CODEX_THREAD_ID",
		"CODEX_CI",
		"CODEX_SANDBOX",
		"GEMINI_CLI",
		"GEMINI_CLI_HOME",
		"GEMINI_CLI_SURFACE",
		"GEMINI_API_KEY",
		"OPENCLAW_CLI",
		"OPENCLAW_SHELL",
		"OPENCODE",
		"OPENCODE_CLIENT",
	} {
		t.Setenv(key, "")
	}
}
