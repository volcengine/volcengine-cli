package cmd

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	mcpclient "github.com/ThinkInAIXYZ/go-mcp/client"
	"github.com/ThinkInAIXYZ/go-mcp/protocol"
	"github.com/ThinkInAIXYZ/go-mcp/transport"
	"github.com/volcengine/volcengine-cli/internal/apisearch"
	"github.com/volcengine/volcengine-cli/internal/scriptrun"
)

// TestMCPStdioRunScriptAndCancellation drives run_script through the real
// stdio transport: a successful run, then an MCP cancellation while an API
// call is in flight, which must kill the Python worker and abort the call.
func TestMCPStdioRunScriptAndCancellation(t *testing.T) {
	if os.Getenv("VOLCENGINE_MCP_SCRIPT_HELPER") == "1" {
		invoker, err := newReloadingVolcengineMCPInvoker(LoadConfig(), "", "")
		if err != nil {
			t.Fatalf("create invoker: %v", err)
		}
		dependencies, err := newMCPToolDependencies(context.Background(), mcpCommandOptions{Transport: mcpTransportStdio, EnableRunScript: true}, invoker)
		if err != nil {
			t.Fatalf("create dependencies: %v", err)
		}
		if err := runMCPStdio(invoker, dependencies); err != nil {
			t.Fatalf("runMCPStdio() error = %v", err)
		}
		return
	}
	if runtime.GOOS == "windows" {
		t.Skip("process liveness check uses POSIX signals")
	}
	python := scriptTestPython(t)
	slowStarted := make(chan struct{}, 1)
	slowCancelled := make(chan struct{}, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		if r.Form.Get("MaxResults") == "99" {
			slowStarted <- struct{}{}
			select {
			case <-r.Context().Done():
				slowCancelled <- struct{}{}
			case <-time.After(30 * time.Second):
			}
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ResponseMetadata":{"RequestId":"req-stdio","Action":"DescribeInstances","Version":"2020-04-01","Service":"ecs","Region":"cn-beijing"},"Result":{"Instances":[{"InstanceId":"i-1"}]}}`))
	}))
	defer server.Close()

	home := t.TempDir()
	pidFile := filepath.Join(t.TempDir(), "worker.pid")
	clientTransport, err := transport.NewStdioClientTransport(
		os.Args[0], []string{"-test.run=^TestMCPStdioRunScriptAndCancellation$"},
		transport.WithStdioClientOptionEnv(
			"VOLCENGINE_MCP_SCRIPT_HELPER=1", "HOME="+home, "USERPROFILE="+home,
			"VOLCENGINE_CLI_CONFIG_FILE="+filepath.Join(home, "missing.json"),
			"VOLCENGINE_ACCESS_KEY=ak-stdio", "VOLCENGINE_SECRET_KEY=sk-stdio", "VOLCENGINE_SESSION_TOKEN=",
			"VOLCENGINE_REGION=cn-beijing", "VOLCENGINE_ENDPOINT="+server.URL, "VOLCENGINE_PROFILE=",
			scriptPythonEnvironment+"="+python,
		),
	)
	if err != nil {
		t.Fatalf("NewStdioClientTransport() error = %v", err)
	}
	client, err := mcpclient.NewClient(clientTransport, mcpclient.WithInitTimeout(20*time.Second))
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}
	defer client.Close()

	tools, err := client.ListTools(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, tool := range tools.Tools {
		found = found || tool.Name == "run_script"
	}
	if !found {
		t.Fatalf("run_script was not registered: %+v", tools.Tools)
	}

	ok, err := client.CallTool(context.Background(), &protocol.CallToolRequest{Name: "run_script", Arguments: map[string]interface{}{
		"code": "page = await call_api('ecs', 'DescribeInstances', {'MaxResults': 1})\nresult = [i['InstanceId'] for i in page['Result']['Instances']] + [args['tag']]\n",
		"args": map[string]interface{}{"tag": "x"},
	}})
	if err != nil || ok.IsError {
		t.Fatalf("run_script result = %#v, error = %v", ok, err)
	}
	var report struct {
		Status string          `json:"status"`
		Result json.RawMessage `json:"result"`
		Calls  []scriptrun.JournalEntry
	}
	if err := json.Unmarshal([]byte(ok.Content[0].(*protocol.TextContent).Text), &report); err != nil {
		t.Fatal(err)
	}
	var values []string
	_ = json.Unmarshal(report.Result, &values)
	if report.Status != scriptrun.RunStatusSucceeded || strings.Join(values, ",") != "i-1,x" || len(report.Calls) != 1 || report.Calls[0].RequestID != "req-stdio" {
		t.Fatalf("report = %+v", report)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	callDone := make(chan error, 1)
	go func() {
		_, err := client.CallTool(ctx, &protocol.CallToolRequest{Name: "run_script", Arguments: map[string]interface{}{
			"code": "import os\nopen(args['pid_file'], 'w').write(str(os.getpid()))\nawait call_api('ecs', 'DescribeInstances', {'MaxResults': 99})\nresult = 'unreachable'\n",
			"args": map[string]interface{}{"pid_file": pidFile},
		}})
		callDone <- err
	}()
	select {
	case <-slowStarted:
	case <-time.After(20 * time.Second):
		t.Fatal("the in-flight API call never started")
	}
	cancel()
	if err := <-callDone; err == nil {
		t.Fatal("cancelled CallTool returned no error")
	}
	select {
	case <-slowCancelled:
	case <-time.After(15 * time.Second):
		t.Fatal("the in-flight API request was not cancelled after MCP cancellation")
	}
	data, err := os.ReadFile(pidFile)
	if err != nil {
		t.Fatal(err)
	}
	pid, _ := strconv.Atoi(strings.TrimSpace(string(data)))
	deadline := time.Now().Add(10 * time.Second)
	for scriptProcessAlive(pid) {
		if time.Now().After(deadline) {
			t.Fatalf("worker %d is still running after MCP cancellation", pid)
		}
		time.Sleep(50 * time.Millisecond)
	}

	// The server stays usable after a cancelled run.
	again, err := client.CallTool(context.Background(), &protocol.CallToolRequest{Name: "run_script", Arguments: map[string]interface{}{"code": "result = 2"}})
	if err != nil || again.IsError {
		t.Fatalf("run after cancellation = %#v, %v", again, err)
	}
}

type fakeAPISearchModule struct {
	options apisearch.SearchOptions
	result  apisearch.SearchResult
	err     error
}

func (m *fakeAPISearchModule) Search(_ context.Context, options apisearch.SearchOptions) (apisearch.SearchResult, error) {
	m.options = options
	return m.result, m.err
}

func TestMCPSearchAPIReturnsFiveFieldRecords(t *testing.T) {
	next := 3
	module := &fakeAPISearchModule{result: apisearch.SearchResult{RequestID: "req-s", Total: 200, Limit: 3, Offset: 0, NextOffset: &next, SkippedRecords: 1, APIs: []apisearch.APIRecord{
		{ServiceCode: "vpc", ServiceName: "私有网络", Action: "DescribeSubnets", Version: "2025-09-01", Title: "DescribeSubnets", Summary: "查询子网列表", FullContent: "调用 DescribeSubnets 接口，查询满足指定条件的子网。", DocumentationURL: "https://example", Score: 0.9},
		{ServiceCode: "vpc", Action: "DescribeSubnets", Version: "2020-04-01", FullContent: "旧版本"},
		{ServiceCode: "vpc", Action: "NotInThisCLI", Version: "2025-09-01"},
	}}}
	registrar := &recordingMCPRegistrar{}
	registerMCPTools(registrar, rootSupport, stubMCPInvoker{}, &fakeDocsModule{}, module, nil)
	result, err := registrar.handlers["search_api"](context.Background(), &protocol.CallToolRequest{Name: "search_api", Arguments: map[string]interface{}{
		"query": "子网", "limit": float64(3), "offset": float64(0),
	}})
	if err != nil || result.IsError {
		t.Fatalf("search_api = %#v, %v", result, err)
	}
	if module.options != (apisearch.SearchOptions{Query: "子网", Limit: 3, Offset: 0}) {
		t.Fatalf("forwarded options = %+v", module.options)
	}
	// Official identities are kept as returned, every version and every
	// record, with exactly five fields each.
	text := result.Content[0].(*protocol.TextContent).Text
	want := `{"Total":200,"NextOffset":3,"APIs":[` +
		`{"action":"DescribeSubnets","service":"vpc","version":"2025-09-01","FullContent":"调用 DescribeSubnets 接口，查询满足指定条件的子网。","score":0.9},` +
		`{"action":"DescribeSubnets","service":"vpc","version":"2020-04-01","FullContent":"旧版本","score":0},` +
		`{"action":"NotInThisCLI","service":"vpc","version":"2025-09-01","FullContent":"","score":0}]}`
	if text != want {
		t.Fatalf("search_api output =\n%s\nwant\n%s", text, want)
	}
	var decoded struct {
		APIs []map[string]interface{} `json:"APIs"`
	}
	if err := json.Unmarshal([]byte(text), &decoded); err != nil {
		t.Fatal(err)
	}
	for _, record := range decoded.APIs {
		if len(record) != 5 {
			t.Fatalf("record has %d fields: %v", len(record), record)
		}
	}

	for _, arguments := range []map[string]interface{}{{}, {"query": "x", "limit": float64(0)}, {"query": "x", "offset": float64(-1)}, {"query": "x", "offset": "3"}} {
		if result, _ := registrar.handlers["search_api"](context.Background(), &protocol.CallToolRequest{Arguments: arguments}); !result.IsError {
			t.Fatalf("arguments %v were accepted", arguments)
		}
	}
	module.err = &apisearch.UpstreamError{StatusCode: 400, Code: "InvalidParam", Message: "bad", RequestID: "req-e"}
	result, err = registrar.handlers["search_api"](context.Background(), &protocol.CallToolRequest{Arguments: map[string]interface{}{"query": "q"}})
	if err != nil || !result.IsError || !strings.Contains(result.Content[0].(*protocol.TextContent).Text, "req-e") {
		t.Fatalf("upstream error result = %#v, %v", result, err)
	}
	// An empty page keeps the same shape.
	module.err, module.result = nil, apisearch.SearchResult{Total: 200, APIs: []apisearch.APIRecord{}}
	result, _ = registrar.handlers["search_api"](context.Background(), &protocol.CallToolRequest{Arguments: map[string]interface{}{"query": "q", "offset": float64(300)}})
	if text := result.Content[0].(*protocol.TextContent).Text; text != `{"Total":200,"APIs":[]}` {
		t.Fatalf("empty page = %s", text)
	}
}

func TestMCPToolDependenciesUseTheOfficialSearchEndpoint(t *testing.T) {
	setupScriptProfile(t, "http://127.0.0.1:1")
	invoker, err := newReloadingVolcengineMCPInvoker(LoadConfig(), "", "")
	if err != nil {
		t.Fatal(err)
	}
	dependencies, err := newMCPToolDependencies(context.Background(), mcpCommandOptions{Transport: mcpTransportStdio}, invoker)
	if err != nil {
		t.Fatal(err)
	}
	if client, ok := dependencies.search.(*apisearch.Client); !ok || client == nil {
		t.Fatalf("search module = %#v", dependencies.search)
	}
}

type fakeScriptRunner struct {
	request scriptrun.Request
	report  *scriptRunReport
	err     error
}

func (r *fakeScriptRunner) RunScript(_ context.Context, request scriptrun.Request) (*scriptRunReport, error) {
	r.request = request
	return r.report, r.err
}

func TestMCPRunScriptToolParsesArgumentsAndFlagsUnsuccessfulRuns(t *testing.T) {
	runner := &fakeScriptRunner{report: &scriptRunReport{Result: &scriptrun.Result{Status: scriptrun.RunStatusSucceeded, Calls: []scriptrun.JournalEntry{}}}}
	registrar := &recordingMCPRegistrar{}
	registerMCPTools(registrar, rootSupport, stubMCPInvoker{}, &fakeDocsModule{}, nil, runner)
	tool := registrar.tools["run_script"]
	if tool == nil || tool.Annotations == nil || !*tool.Annotations.DestructiveHint || *tool.Annotations.ReadOnlyHint || !strings.Contains(tool.Description, "not a sandbox") {
		t.Fatalf("run_script tool = %#v", tool)
	}
	handler := registrar.handlers["run_script"]
	result, err := handler(context.Background(), &protocol.CallToolRequest{Arguments: map[string]interface{}{
		"code": "result = args", "args": map[string]interface{}{"a": float64(1)}, "max_calls": float64(7), "timeout_seconds": float64(9),
	}})
	if err != nil || result.IsError {
		t.Fatalf("run_script = %#v, %v", result, err)
	}
	if runner.request.Code != "result = args" || string(runner.request.Args) != `{"a":1}` || runner.request.Limits.MaxCalls != 7 || runner.request.Limits.Timeout != 9*time.Second {
		t.Fatalf("request = %+v", runner.request)
	}
	runner.report.Status = scriptrun.RunStatusLimitExceeded
	if result, _ := handler(context.Background(), &protocol.CallToolRequest{Arguments: map[string]interface{}{"code": "x"}}); !result.IsError || !strings.Contains(result.Content[0].(*protocol.TextContent).Text, "limit_exceeded") {
		t.Fatalf("unsuccessful run result = %#v", result)
	}
	for _, arguments := range []map[string]interface{}{{}, {"code": 1}, {"code": "x", "max_calls": float64(0)}, {"code": "x", "max_concurrency": float64(100)}} {
		if result, _ := handler(context.Background(), &protocol.CallToolRequest{Arguments: arguments}); !result.IsError {
			t.Fatalf("arguments %v were accepted", arguments)
		}
	}
}

// Discovery prefers search_api; list_services/list_apis are the fallback when
// search fails or finds nothing this CLI can call.
func TestMCPDiscoveryPrefersSearchAPIWithListFallback(t *testing.T) {
	module := &fakeAPISearchModule{}
	registrar := &recordingMCPRegistrar{}
	registerMCPTools(registrar, rootSupport, stubMCPInvoker{}, &fakeDocsModule{}, module, nil)
	if registrar.order[0] != "search_api" {
		t.Fatalf("tool order = %v, want search_api first", registrar.order)
	}
	for name, want := range map[string]string{"search_api": "list_services", "list_services": "search_api", "list_apis": "search_api"} {
		if !strings.Contains(registrar.tools[name].Description, want) {
			t.Fatalf("%s description does not mention %s: %q", name, want, registrar.tools[name].Description)
		}
	}

	text := func(arguments map[string]interface{}) (string, bool) {
		result, err := registrar.handlers["search_api"](context.Background(), &protocol.CallToolRequest{Arguments: arguments})
		if err != nil {
			t.Fatal(err)
		}
		return result.Content[0].(*protocol.TextContent).Text, result.IsError
	}
	module.err = errors.New("network down")
	if body, isError := text(map[string]interface{}{"query": "q"}); !isError || !strings.Contains(body, "network down") || !strings.Contains(body, "list_services") {
		t.Fatalf("search failure = %q", body)
	}
}
