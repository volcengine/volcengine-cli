package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/volcengine/volcengine-cli/internal/docs"
)

func TestDocsSearchCommandDefaultsAndRepeatedServiceCodes(t *testing.T) {
	fake := &fakeDocsModule{searchResult: docs.SearchResult{Documents: []docs.SearchDocument{}}}
	stdout, stderr, err := executeDocsCommand(t, fake,
		"search", "对象存储",
		"--service-code", " tos ",
		"--service-code", "",
		"--service-code", "ecs",
		"--profile", "missing-profile",
		"--region", "moon-1",
		"--endpoint", "ignored.example.com",
		"--output", "json",
	)
	if err != nil {
		t.Fatalf("execute: %v; stderr=%s", err, stderr)
	}
	want := docs.SearchOptions{
		Query:         "对象存储",
		Limit:         docs.DefaultSearchLimit,
		ServiceCodes:  []string{" tos ", "", "ecs"},
		SnippetLength: docs.DefaultSnippetLength,
	}
	if !reflect.DeepEqual(fake.searchOptions, want) {
		t.Fatalf("SearchOptions = %#v, want %#v", fake.searchOptions, want)
	}
	if fake.searchCalls != 1 || fake.fetchCalls != 0 {
		t.Fatalf("calls: search=%d fetch=%d", fake.searchCalls, fake.fetchCalls)
	}
	var structured map[string]interface{}
	if err := json.Unmarshal([]byte(stdout), &structured); err != nil {
		t.Fatalf("stdout is not JSON: %v\n%s", err, stdout)
	}
	if _, exists := structured["documents"]; !exists {
		t.Fatalf("stdout missing documents: %s", stdout)
	}
	if stderr != "" {
		t.Fatalf("successful command wrote stderr: %q", stderr)
	}
}

func TestDocsSearchTextGolden(t *testing.T) {
	fake := &fakeDocsModule{searchResult: docs.SearchResult{
		RequestID: "request-id-not-rendered-in-text",
		Documents: []docs.SearchDocument{
			{Title: "计费概述", URL: "https://docs.volcengine.com/docs/6349/78455", ServiceCodes: []string{"tos"}, Snippet: "第一段摘要"},
			{Title: "", URL: "https://www.volcengine.com/docs/6349/2", ServiceCodes: []string{"tos", "billing"}, Snippet: "第二段摘要"},
		},
	}}
	restore := setLanguageForTest(LanguageSimplifiedChinese)
	defer restore()
	stdout, stderr, err := executeDocsCommand(t, fake, "search", "计费", "--limit", "2", "--snippet-length", "20", "--output", "text")
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	want := `## 1. 计费概述
https://docs.volcengine.com/docs/6349/78455
ServiceCodes: tos

第一段摘要

---

## 2. 无标题
https://www.volcengine.com/docs/6349/2
ServiceCodes: tos, billing

第二段摘要
`
	if stdout != want {
		t.Fatalf("text output:\n%s\nwant:\n%s", stdout, want)
	}
	if stderr != "" {
		t.Fatalf("stderr = %q", stderr)
	}
	if fake.searchOptions.Limit != 2 || fake.searchOptions.SnippetLength != 20 {
		t.Fatalf("SearchOptions = %#v", fake.searchOptions)
	}
}

func TestDocsFetchTextGoldenAndLanguageFlag(t *testing.T) {
	next := 300
	fake := &fakeDocsModule{fetchResult: docs.FetchResult{
		RequestID:           "request-id",
		Title:               "快速使用 TOS Browser",
		URL:                 "https://www.volcengine.com/docs/6349/162514",
		Content:             "分页正文",
		StartIndex:          0,
		NextStartIndex:      &next,
		RemainingCharacters: 1200,
		HasMore:             true,
		HasContent:          true,
	}}
	restore := setLanguageForTest(LanguageEnglish)
	defer restore()
	stdout, stderr, err := executeDocsCommand(t, fake,
		"fetch", "https://www.volcengine.com/docs/6349/162514?lang=zh",
		"--start-index", "0", "--max-length", "300", "--output", "text", "--lang", "ZH",
	)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	want := `# 快速使用 TOS Browser
https://www.volcengine.com/docs/6349/162514

分页正文

【续读】还有 1200 字。重新调用 fetch 并加参数 --start-index 300 继续读。
`
	if stdout != want {
		t.Fatalf("text output:\n%s\nwant:\n%s", stdout, want)
	}
	if stderr != "" {
		t.Fatalf("stderr = %q", stderr)
	}
	wantOptions := docs.FetchOptions{
		URL:        "https://www.volcengine.com/docs/6349/162514?lang=zh",
		StartIndex: 0,
		MaxLength:  300,
	}
	if !reflect.DeepEqual(fake.fetchOptions, wantOptions) {
		t.Fatalf("FetchOptions = %#v, want %#v", fake.fetchOptions, wantOptions)
	}
}

func TestDocsFetchEmptyContentText(t *testing.T) {
	fake := &fakeDocsModule{fetchResult: docs.FetchResult{
		URL: "https://www.volcengine.com/docs/6349", HasContent: false,
	}}
	restore := setLanguageForTest(LanguageSimplifiedChinese)
	defer restore()
	stdout, _, err := executeDocsCommand(t, fake, "fetch", "https://www.volcengine.com/docs/6349", "--output", "text")
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	want := "https://www.volcengine.com/docs/6349\n\n该链接没有具体文章正文。\n"
	if stdout != want {
		t.Fatalf("stdout = %q, want %q", stdout, want)
	}
}

func TestDocsFetchStructuredOutputPinsPaginationFields(t *testing.T) {
	fake := &fakeDocsModule{fetchResult: docs.FetchResult{
		RequestID:  "request-id",
		Title:      "导航页",
		URL:        "https://www.volcengine.com/docs/6349",
		StartIndex: 0,
		HasContent: false,
	}}
	stdout, _, err := executeDocsCommand(t, fake, "fetch", "https://www.volcengine.com/docs/6349", "--output", "json")
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	var got map[string]interface{}
	if err := json.Unmarshal([]byte(stdout), &got); err != nil {
		t.Fatalf("decode output: %v", err)
	}
	for key, want := range map[string]interface{}{
		"request_id":           "request-id",
		"next_start_index":     nil,
		"remaining_characters": float64(0),
		"has_more":             false,
		"has_content":          false,
	} {
		if value, exists := got[key]; !exists || !reflect.DeepEqual(value, want) {
			t.Errorf("%s = %#v (exists=%v), want %#v", key, value, exists, want)
		}
	}
}

func TestDocsQueryRemainsJMESPathOutputControl(t *testing.T) {
	fake := &fakeDocsModule{searchResult: docs.SearchResult{Documents: []docs.SearchDocument{{Title: "命中标题"}}}}
	stdout, _, err := executeDocsCommand(t, fake,
		"search", "query text", "--query", "documents[0].title", "--output", "text",
	)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if stdout != "命中标题\n" {
		t.Fatalf("query output = %q", stdout)
	}
	if fake.searchOptions.Query != "query text" {
		t.Fatalf("positional search query = %q", fake.searchOptions.Query)
	}
}

func TestDocsStructuredOutputFormats(t *testing.T) {
	tests := []struct {
		name   string
		format string
		want   string
	}{
		{name: "json", format: "json", want: `"request_id": "r"`},
		{name: "yaml", format: "yaml", want: "request_id: r"},
		{name: "table", format: "table", want: "documents"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			fake := &fakeDocsModule{searchResult: docs.SearchResult{RequestID: "r", Documents: []docs.SearchDocument{{Title: "t", URL: "u", ServiceCodes: []string{}, Snippet: "s"}}}}
			stdout, _, err := executeDocsCommand(t, fake, "search", "q", "--output", test.format)
			if err != nil {
				t.Fatalf("execute: %v", err)
			}
			if !strings.Contains(stdout, test.want) {
				t.Fatalf("%s output missing %q:\n%s", test.format, test.want, stdout)
			}
		})
	}
}

func TestDocsRejectsArgumentsAndFormatsBeforeRequest(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want string
	}{
		{name: "missing search text", args: []string{"search"}, want: "search text is required"},
		{name: "too many fetch URLs", args: []string{"fetch", "one", "two"}, want: "expected one document URL"},
		{name: "search format", args: []string{"search", "q", "--output", "off"}, want: "unsupported output format"},
		{name: "fetch format", args: []string{"fetch", "https://www.volcengine.com/docs/1/2", "--output", "table"}, want: "unsupported output format"},
		{name: "invalid query", args: []string{"search", "q", "--query", "["}, want: "invalid --query"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			fake := &fakeDocsModule{}
			stdout, _, err := executeDocsCommand(t, fake, test.args...)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("error = %v, want substring %q", err, test.want)
			}
			if stdout != "" {
				t.Fatalf("error path wrote stdout: %q", stdout)
			}
			if fake.searchCalls != 0 || fake.fetchCalls != 0 {
				t.Fatalf("invalid command made request: search=%d fetch=%d", fake.searchCalls, fake.fetchCalls)
			}
		})
	}
}

func TestDocsModuleErrorsDoNotWriteStdout(t *testing.T) {
	fake := &fakeDocsModule{searchErr: errors.New("network failed")}
	stdout, _, err := executeDocsCommand(t, fake, "search", "q")
	if err == nil || err.Error() != "network failed" {
		t.Fatalf("error = %v", err)
	}
	if stdout != "" {
		t.Fatalf("stdout = %q", stdout)
	}
}

func TestDocsHelpShowsInterfaceAndDefaults(t *testing.T) {
	fake := &fakeDocsModule{}
	stdout, _, err := executeDocsCommand(t, fake, "search", "--help")
	if err != nil {
		t.Fatalf("help: %v", err)
	}
	for _, expected := range []string{"docs search <text> [flags]", "--limit int", "(default 10)", "--service-code stringArray", "--snippet-length int", "--output string"} {
		if !strings.Contains(stdout, expected) {
			t.Errorf("help missing %q:\n%s", expected, stdout)
		}
	}
	assertDocsHelpHidesCompatibilityFlags(t, stdout)
	if fake.searchCalls != 0 {
		t.Fatalf("help made %d search calls", fake.searchCalls)
	}

	stdout, _, err = executeDocsCommand(t, fake, "fetch", "--help")
	if err != nil {
		t.Fatalf("fetch help: %v", err)
	}
	for _, expected := range []string{"docs fetch <url> [flags]", "--start-index int", "--max-length int", "(default 5000)", "--output string"} {
		if !strings.Contains(stdout, expected) {
			t.Errorf("fetch help missing %q:\n%s", expected, stdout)
		}
	}
	assertDocsHelpHidesCompatibilityFlags(t, stdout)
	if fake.fetchCalls != 0 {
		t.Fatalf("help made %d fetch calls", fake.fetchCalls)
	}
}

func assertDocsHelpHidesCompatibilityFlags(t *testing.T, help string) {
	t.Helper()
	for _, forbidden := range []string{"--query", "Global Flags:", "--profile", "--region", "--endpoint", "--lang"} {
		if strings.Contains(help, forbidden) {
			t.Errorf("help unexpectedly exposes %q:\n%s", forbidden, help)
		}
	}
}

func TestDocsUserAgentMatchesNormalServiceCLIUserAgent(t *testing.T) {
	tests := []struct {
		name     string
		env      map[string]string
		metadata string
	}{
		{
			name:     "automatic detection",
			env:      map[string]string{"CODEX_THREAD_ID": "thread"},
			metadata: "skill-invoker/codex",
		},
		{
			name: "explicit caller and skill without caller type",
			env: map[string]string{
				"VE_CALLER_NAME": "custom-agent",
				"VE_SKILL_NAME":  "volcengine-docs",
			},
			metadata: "skill-invoker/custom-agent; skill/volcengine-docs",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			getenv := testEnv(tt.env)
			got := docsUserAgent(getenv)
			want := clientUserAgent(getenv)
			if got != want || !strings.Contains(got, tt.metadata) {
				t.Fatalf("docs User-Agent = %q, normal CLI User-Agent = %q, want metadata %q", got, want, tt.metadata)
			}
		})
	}
}

type fakeDocsModule struct {
	searchOptions docs.SearchOptions
	fetchOptions  docs.FetchOptions
	searchResult  docs.SearchResult
	fetchResult   docs.FetchResult
	searchErr     error
	fetchErr      error
	searchCalls   int
	fetchCalls    int
}

func (f *fakeDocsModule) Search(_ context.Context, options docs.SearchOptions) (docs.SearchResult, error) {
	f.searchCalls++
	f.searchOptions = options
	return f.searchResult, f.searchErr
}

func (f *fakeDocsModule) Fetch(_ context.Context, options docs.FetchOptions) (docs.FetchResult, error) {
	f.fetchCalls++
	f.fetchOptions = options
	return f.fetchResult, f.fetchErr
}

func executeDocsCommand(t *testing.T, module docs.Module, args ...string) (stdout, stderr string, err error) {
	t.Helper()
	command := newDocsCommand(module)
	command.SilenceErrors = true
	command.SilenceUsage = true
	var stdoutBuffer, stderrBuffer bytes.Buffer
	command.SetOut(&stdoutBuffer)
	command.SetErr(&stderrBuffer)
	command.SetArgs(args)
	_, err = command.ExecuteC()
	return stdoutBuffer.String(), stderrBuffer.String(), err
}
