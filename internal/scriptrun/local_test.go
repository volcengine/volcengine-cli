package scriptrun

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

// testPython returns a real CPython interpreter for integration tests. Set
// VOLCENGINE_TEST_PYTHON to pin one; otherwise python3 from PATH is used.
func testPython(t *testing.T) string {
	t.Helper()
	path := os.Getenv("VOLCENGINE_TEST_PYTHON")
	if path == "" {
		found, err := exec.LookPath("python3")
		if err != nil {
			t.Skip("python3 is not available; set VOLCENGINE_TEST_PYTHON to run real worker tests")
		}
		path = found
	}
	absolute, err := filepath.Abs(path)
	if err != nil {
		t.Fatal(err)
	}
	return absolute
}

type invokeFunc func(ctx context.Context, call Call) (map[string]interface{}, *CallError)

func (f invokeFunc) Invoke(ctx context.Context, call Call) (map[string]interface{}, *CallError) {
	return f(ctx, call)
}

func runScript(t *testing.T, code string, args interface{}, limits Limits, invoker Invoker) *Result {
	t.Helper()
	return runScriptContext(t, context.Background(), code, args, limits, invoker)
}

func runScriptContext(t *testing.T, ctx context.Context, code string, args interface{}, limits Limits, invoker Invoker) *Result {
	t.Helper()
	rawArgs, err := json.Marshal(args)
	if err != nil {
		t.Fatal(err)
	}
	executor := &LocalExecutor{Python: testPython(t)}
	result, err := executor.Execute(ctx, Request{Code: code, Args: rawArgs, Limits: limits}, invoker)
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	return result
}

func decodeResult(t *testing.T, result *Result, target interface{}) {
	t.Helper()
	if result.Status != RunStatusSucceeded {
		t.Fatalf("status = %s, error = %#v, output = %q", result.Status, result.Error, result.Output)
	}
	if err := json.Unmarshal(result.Result, target); err != nil {
		t.Fatalf("decode result %s: %v", result.Result, err)
	}
}

// pagedInvoker serves ListInstances in pages of two and DescribeInstance for
// each instance, so scripts must chain dependent calls.
func pagedInvoker(total int) invokeFunc {
	return func(_ context.Context, call Call) (map[string]interface{}, *CallError) {
		switch call.API {
		case "ListInstances":
			offset := 0
			if token, ok := call.Parameters["NextToken"].(string); ok && token != "" {
				offset, _ = strconv.Atoi(token)
			}
			items := []interface{}{}
			for index := offset; index < offset+2 && index < total; index++ {
				items = append(items, map[string]interface{}{"InstanceId": fmt.Sprintf("i-%d", index)})
			}
			next := ""
			if offset+2 < total {
				next = strconv.Itoa(offset + 2)
			}
			return map[string]interface{}{"Result": map[string]interface{}{"Instances": items, "NextToken": next}}, nil
		case "DescribeInstance":
			id, _ := call.Parameters["InstanceId"].(string)
			index, _ := strconv.Atoi(strings.TrimPrefix(id, "i-"))
			status := "Running"
			if index%2 == 1 {
				status = "Stopped"
			}
			return map[string]interface{}{"Result": map[string]interface{}{"InstanceId": id, "Status": status, "Cores": json.Number(strconv.Itoa(index + 1))}}, nil
		}
		return nil, &CallError{Code: "UnsupportedAPI", Status: CallStatusRejected, Message: call.API}
	}
}

func TestLocalExecutorRunsLoopsBranchesAndDependentPagedCalls(t *testing.T) {
	code := `
instances = []
token = ""
while True:
    page = await call_api("ecs", "ListInstances", {"NextToken": token, "MaxResults": args["page_size"]}, region="cn-shanghai")
    instances.extend(page["Result"]["Instances"])
    token = page["Result"]["NextToken"]
    if not token:
        break

running, stopped_cores = [], 0
for item in instances:
    detail = await call_api("ecs", "DescribeInstance", {"InstanceId": item["InstanceId"]})
    if detail["Result"]["Status"] == "Running":
        running.append(item["InstanceId"])
    else:
        stopped_cores += detail["Result"]["Cores"]
print("scanned", len(instances))
result = {"running": running, "stopped_cores": stopped_cores, "total": len(instances)}
`
	result := runScript(t, code, map[string]interface{}{"page_size": 2}, DefaultLimits(), pagedInvoker(5))
	var got struct {
		Running      []string `json:"running"`
		StoppedCores int      `json:"stopped_cores"`
		Total        int      `json:"total"`
	}
	decodeResult(t, result, &got)
	if got.Total != 5 || strings.Join(got.Running, ",") != "i-0,i-2,i-4" || got.StoppedCores != 2+4 {
		t.Fatalf("unexpected result %+v", got)
	}
	if result.Summary.Total != 3+5 || result.Summary.OK != 8 {
		t.Fatalf("summary = %+v", result.Summary)
	}
	if result.Calls[0].API != "ListInstances" || result.Calls[0].Region != "cn-shanghai" || result.Calls[3].API != "DescribeInstance" || result.Calls[7].Seq != 8 {
		t.Fatalf("journal order = %+v", result.Calls)
	}
	if !strings.Contains(result.Output, "scanned 5") {
		t.Fatalf("output = %q", result.Output)
	}
}

func TestLocalExecutorBoundsConcurrencyAndHandlesOutOfOrderCompletion(t *testing.T) {
	var mu sync.Mutex
	active, peak := 0, 0
	invoker := invokeFunc(func(ctx context.Context, call Call) (map[string]interface{}, *CallError) {
		mu.Lock()
		active++
		if active > peak {
			peak = active
		}
		mu.Unlock()
		defer func() {
			mu.Lock()
			active--
			mu.Unlock()
		}()
		index, _ := call.Parameters["Index"].(json.Number).Int64()
		// Earlier calls finish later, so responses arrive out of order.
		select {
		case <-time.After(time.Duration(8-index) * 40 * time.Millisecond):
		case <-ctx.Done():
			return nil, &CallError{Code: "Cancelled", Status: CallStatusUnknown, Message: ctx.Err().Error()}
		}
		return map[string]interface{}{"Index": index}, nil
	})
	code := `
import asyncio
responses = await asyncio.gather(*[call_api("svc", "Echo", {"Index": i}) for i in range(8)])
result = [r["Index"] for r in responses]
`
	limits := DefaultLimits()
	limits.MaxConcurrency = 3
	result := runScript(t, code, nil, limits, invoker)
	var got []int
	decodeResult(t, result, &got)
	for index, value := range got {
		if value != index {
			t.Fatalf("responses were not matched to their requests: %v", got)
		}
	}
	if peak != 3 {
		t.Fatalf("peak in-flight calls = %d, want 3", peak)
	}
}

func TestLocalExecutorEnforcesCallLimitPerCall(t *testing.T) {
	code := `
codes = []
for i in range(3):
    try:
        await call_api("svc", "Echo", {"I": i})
        codes.append("ok")
    except APIError as error:
        codes.append(error.code + "/" + error.status)
result = codes
`
	limits := DefaultLimits()
	limits.MaxCalls = 2
	result := runScript(t, code, nil, limits, invokeFunc(func(context.Context, Call) (map[string]interface{}, *CallError) {
		return map[string]interface{}{}, nil
	}))
	var got []string
	decodeResult(t, result, &got)
	want := "ok,ok,CallLimitExceeded/rejected"
	if strings.Join(got, ",") != want {
		t.Fatalf("codes = %v, want %s", got, want)
	}
	if result.Summary.OK != 2 || result.Summary.Rejected != 1 {
		t.Fatalf("summary = %+v", result.Summary)
	}
}

// A script that keeps calling after its budget is spent must not grow the
// journal or the final payload without bound: the first over-budget attempt
// is rejected so the script can react, and any further attempt ends the run.
func TestLocalExecutorAttemptBudgetBoundsJournalAndEndsRun(t *testing.T) {
	limits := DefaultLimits()
	limits.MaxCalls = 1
	code := `
for i in range(2000):
    try:
        await call_api("svc", "Echo", {"I": i})
    except APIError:
        pass
result = "finished"
`
	result := runScript(t, code, nil, limits, invokeFunc(func(context.Context, Call) (map[string]interface{}, *CallError) {
		return map[string]interface{}{}, nil
	}))
	if result.Status != RunStatusLimitExceeded || result.Error == nil || result.Error.Type != ErrorCodeCallLimitExceeded {
		t.Fatalf("status = %s, error = %+v", result.Status, result.Error)
	}
	if len(result.Calls) != limits.MaxCalls+1 || result.Calls[0].Status != CallStatusOK || result.Calls[1].ErrorCode != ErrorCodeCallLimitExceeded {
		t.Fatalf("journal has %d entries: %+v", len(result.Calls), result.Calls)
	}
	if len(result.Result) != 0 {
		t.Fatalf("a limit-exceeded run must not report a result: %s", result.Result)
	}

	// Concurrent over-budget calls are bounded the same way.
	limits.MaxCalls = 3
	code = `
import asyncio
await asyncio.gather(*[call_api("svc", "Echo", {"I": i}) for i in range(500)], return_exceptions=True)
result = "finished"
`
	result = runScript(t, code, nil, limits, invokeFunc(func(context.Context, Call) (map[string]interface{}, *CallError) {
		return map[string]interface{}{}, nil
	}))
	if result.Status != RunStatusLimitExceeded || len(result.Calls) > limits.MaxCalls+1 {
		t.Fatalf("status = %s, journal entries = %d", result.Status, len(result.Calls))
	}
}

func TestLocalExecutorReportsAPIErrorsAndScriptFailures(t *testing.T) {
	invoker := invokeFunc(func(_ context.Context, call Call) (map[string]interface{}, *CallError) {
		if call.API == "Deny" {
			return nil, &CallError{Code: "SafetyDenied", Status: CallStatusRejected, Message: "blocked by rule ecs:Delete*"}
		}
		return nil, &CallError{Code: "InvalidParameter", Status: CallStatusFailed, Message: "bad value", RequestID: "req-1"}
	})
	code := `
try:
    await call_api("ecs", "Deny")
except APIError as error:
    denied = (error.code, error.status, error.seq)
try:
    await call_api("ecs", "Fail")
except APIError as error:
    failed = (error.code, error.status, error.request_id, str(error))
if denied[0] == "SafetyDenied":
    raise ValueError("stop after %s and %s" % (denied, failed))
`
	result := runScript(t, code, nil, DefaultLimits(), invoker)
	if result.Status != RunStatusFailed || result.Error == nil || result.Error.Type != "ValueError" {
		t.Fatalf("result = %+v", result)
	}
	for _, want := range []string{"SafetyDenied", "'rejected', 1", "InvalidParameter", "req-1", "bad value"} {
		if !strings.Contains(result.Error.Message, want) {
			t.Fatalf("error message %q does not contain %q", result.Error.Message, want)
		}
	}
	if !strings.Contains(result.Error.Traceback, `File "<script>", line 11`) || strings.Contains(result.Error.Traceback, "asyncio") || strings.Contains(result.Error.Traceback, `File "<string>"`) {
		t.Fatalf("traceback = %q", result.Error.Traceback)
	}
	if result.Calls[0].Status != CallStatusRejected || result.Calls[1].Status != CallStatusFailed || result.Calls[1].RequestID != "req-1" {
		t.Fatalf("journal = %+v", result.Calls)
	}
}

func TestLocalExecutorStdoutCannotCorruptFraming(t *testing.T) {
	code := `
import os, subprocess, sys
print("hello from print")
sys.stdout.write("raw stdout\n")
sys.stdout.flush()
os.write(1, b"\x00\x00\x00\x05junk\n")
sys.stdout.buffer.write(b"\xff\xfe binary\n")
sys.stdout.flush()
subprocess.run([sys.executable, "-c", "print('child stdout')"], check=True)
print("to stderr", file=sys.stderr)
data = await call_api("svc", "Echo", {"Value": "intact"})
result = data
`
	result := runScript(t, code, nil, DefaultLimits(), invokeFunc(func(_ context.Context, call Call) (map[string]interface{}, *CallError) {
		return call.Parameters, nil
	}))
	var got map[string]string
	decodeResult(t, result, &got)
	if got["Value"] != "intact" {
		t.Fatalf("result = %v", got)
	}
	for _, want := range []string{"hello from print", "raw stdout", "junk", "binary", "child stdout", "to stderr"} {
		if !strings.Contains(result.Output, want) {
			t.Fatalf("output %q does not contain %q", result.Output, want)
		}
	}
}

func TestLocalExecutorTruncatesOutputAndRejectsInvalidResults(t *testing.T) {
	limits := DefaultLimits()
	limits.MaxOutputBytes = 1024
	result := runScript(t, "print('x' * 100000)\nresult = 1\n", nil, limits, invokeFunc(nil))
	if result.Status != RunStatusSucceeded || !result.OutputTruncated || len(result.Output) != 1024 {
		t.Fatalf("status = %s, truncated = %v, output length = %d", result.Status, result.OutputTruncated, len(result.Output))
	}

	result = runScript(t, "result = {1, 2}\n", nil, DefaultLimits(), invokeFunc(nil))
	if result.Status != RunStatusFailed || result.Error == nil || result.Error.Type != "ResultError" {
		t.Fatalf("non-JSON result = %+v", result)
	}

	limits = DefaultLimits()
	limits.MaxResultBytes = 1024
	result = runScript(t, "result = 'y' * 5000\n", nil, limits, invokeFunc(nil))
	if result.Status != RunStatusFailed || result.Error == nil || result.Error.Type != "ResultTooLarge" {
		t.Fatalf("large result = %+v", result)
	}

	limits = DefaultLimits()
	limits.MaxFrameBytes = 64 * 1024
	limits.MaxResultBytes = 1024
	result = runScript(t, "result = 'z' * 200000\n", nil, limits, invokeFunc(nil))
	if result.Status != RunStatusFailed || result.Error == nil || result.Error.Type != "ProtocolError" {
		t.Fatalf("oversized frame = %+v", result)
	}
}

func TestLocalExecutorDoesNotDeliverOversizedResponses(t *testing.T) {
	limits := DefaultLimits()
	limits.MaxFrameBytes = 16 * 1024
	limits.MaxResultBytes = 1024
	code := `
try:
    await call_api("svc", "Big")
except APIError as error:
    result = error.code + "/" + error.status
`
	result := runScript(t, code, nil, limits, invokeFunc(func(context.Context, Call) (map[string]interface{}, *CallError) {
		return map[string]interface{}{"Blob": strings.Repeat("b", 64*1024)}, nil
	}))
	var got string
	decodeResult(t, result, &got)
	if got != "ResponseTooLarge/ok" || result.Calls[0].Status != CallStatusOK || result.Calls[0].ErrorCode != "ResponseTooLarge" {
		t.Fatalf("got %q, journal %+v", got, result.Calls)
	}
}

func TestLocalExecutorPerCallTimeoutIsUnknownAndScriptContinues(t *testing.T) {
	limits := DefaultLimits()
	limits.CallTimeout = 200 * time.Millisecond
	invoker := invokeFunc(func(ctx context.Context, call Call) (map[string]interface{}, *CallError) {
		if call.API == "Slow" {
			<-ctx.Done()
			return nil, &CallError{Code: "RequestError", Status: CallStatusUnknown, Message: ctx.Err().Error()}
		}
		return map[string]interface{}{"fast": true}, nil
	})
	code := `
try:
    await call_api("svc", "Slow")
except APIError as error:
    slow = error.code + "/" + error.status
fast = await call_api("svc", "Fast")
result = [slow, fast["fast"]]
`
	result := runScript(t, code, nil, limits, invoker)
	var got []interface{}
	decodeResult(t, result, &got)
	if got[0] != "CallTimeout/unknown" || got[1] != true || result.Calls[0].Status != CallStatusUnknown {
		t.Fatalf("got %v, journal %+v", got, result.Calls)
	}
}

func TestLocalExecutorFatalCallErrorAbortsRun(t *testing.T) {
	invoker := invokeFunc(func(_ context.Context, call Call) (map[string]interface{}, *CallError) {
		if call.API == "Second" {
			return nil, &CallError{Code: "IdentityChanged", Status: CallStatusRejected, Message: "credential identity changed", Fatal: true}
		}
		return map[string]interface{}{}, nil
	})
	code := `
await call_api("svc", "First")
try:
    await call_api("svc", "Second")
except APIError:
    pass
await call_api("svc", "Third")
result = "should not finish"
`
	result := runScript(t, code, nil, DefaultLimits(), invoker)
	if result.Status != RunStatusAborted || result.Error == nil || !strings.Contains(result.Error.Message, "IdentityChanged") {
		t.Fatalf("result = %+v", result)
	}
	if len(result.Calls) > 3 || result.Calls[0].Status != CallStatusOK || result.Calls[1].Status != CallStatusRejected {
		t.Fatalf("journal = %+v", result.Calls)
	}
}

func TestLocalExecutorCancellationKillsProcessTreeAndJournalsUnknown(t *testing.T) {
	pidFile := filepath.Join(t.TempDir(), "pids")
	started := make(chan struct{})
	var once sync.Once
	invoker := invokeFunc(func(ctx context.Context, call Call) (map[string]interface{}, *CallError) {
		if call.API == "Done" {
			return map[string]interface{}{}, nil
		}
		once.Do(func() { close(started) })
		<-ctx.Done()
		return nil, &CallError{Code: "RequestError", Status: CallStatusUnknown, Message: ctx.Err().Error()}
	})
	code := `
import subprocess, sys, os
await call_api("svc", "Done")
child = subprocess.Popen([sys.executable, "-c", "import time; time.sleep(60)"])
with open(args["pid_file"], "w") as handle:
    handle.write("%d %d" % (os.getpid(), child.pid))
await call_api("ecs", "RunInstances", {"Count": 1})
result = "unreachable"
`
	ctx, cancel := context.WithCancel(context.Background())
	resultChannel := make(chan *Result, 1)
	go func() {
		resultChannel <- runScriptContext(t, ctx, code, map[string]string{"pid_file": pidFile}, DefaultLimits(), invoker)
	}()
	select {
	case <-started:
	case <-time.After(20 * time.Second):
		t.Fatal("the slow call never started")
	}
	cancel()
	var result *Result
	select {
	case result = <-resultChannel:
	case <-time.After(20 * time.Second):
		t.Fatal("Execute did not return after cancellation")
	}
	if result.Status != RunStatusCancelled {
		t.Fatalf("status = %s, error = %+v", result.Status, result.Error)
	}
	if len(result.Calls) != 2 || result.Calls[0].Status != CallStatusOK || result.Calls[1].Status != CallStatusUnknown || result.Calls[1].ErrorCode != ErrorCodeRunCancelled {
		t.Fatalf("journal = %+v", result.Calls)
	}
	assertProcessesGone(t, pidFile)
}

func TestLocalExecutorWallClockTimeoutStopsBusyScript(t *testing.T) {
	pidFile := filepath.Join(t.TempDir(), "pids")
	limits := DefaultLimits()
	limits.Timeout = 1500 * time.Millisecond
	code := `
import os, subprocess, sys
child = subprocess.Popen([sys.executable, "-c", "import time; time.sleep(60)"])
with open(args["pid_file"], "w") as handle:
    handle.write("%d %d" % (os.getpid(), child.pid))
while True:
    pass
`
	began := time.Now()
	result := runScript(t, code, map[string]string{"pid_file": pidFile}, limits, invokeFunc(nil))
	if result.Status != RunStatusTimedOut {
		t.Fatalf("status = %s, error = %+v", result.Status, result.Error)
	}
	if elapsed := time.Since(began); elapsed > 10*time.Second {
		t.Fatalf("timeout took %s", elapsed)
	}
	assertProcessesGone(t, pidFile)
}

func TestLocalExecutorDoesNotPassCredentialsToWorker(t *testing.T) {
	for _, name := range []string{"VOLCENGINE_ACCESS_KEY", "VOLCENGINE_SECRET_KEY", "VOLCENGINE_SESSION_TOKEN", "PYTHONPATH"} {
		previous, existed := os.LookupEnv(name)
		_ = os.Setenv(name, "leak-"+name)
		defer func(name, previous string, existed bool) {
			if existed {
				_ = os.Setenv(name, previous)
			} else {
				_ = os.Unsetenv(name)
			}
		}(name, previous, existed)
	}
	code := `
import os, sys
result = {"env": sorted(k for k in os.environ if k.startswith(("VOLCENGINE", "PYTHON"))), "cwd_empty": os.listdir(".") == [], "path0": sys.path[0] if sys.path else ""}
`
	result := runScript(t, code, nil, DefaultLimits(), invokeFunc(nil))
	var got struct {
		Env      []string `json:"env"`
		CwdEmpty bool     `json:"cwd_empty"`
		Path0    string   `json:"path0"`
	}
	decodeResult(t, result, &got)
	if len(got.Env) != 0 || !got.CwdEmpty || got.Path0 == "" {
		t.Fatalf("worker environment = %+v", got)
	}
}

func TestLocalExecutorValidatesRequests(t *testing.T) {
	executor := &LocalExecutor{Python: testPython(t)}
	limits := DefaultLimits()
	cases := []struct {
		name    string
		request Request
		want    string
	}{
		{"empty code", Request{Code: " ", Limits: limits}, "code is required"},
		{"bad args", Request{Code: "x = 1", Args: json.RawMessage("{"), Limits: limits}, "args must be valid JSON"},
		{"bad limits", Request{Code: "x = 1", Limits: Limits{}}, "max_calls"},
		{"code too large", Request{Code: strings.Repeat("#", limits.MaxCodeBytes+1), Limits: limits}, "max_code_bytes"},
	}
	for _, testCase := range cases {
		if _, err := executor.Execute(context.Background(), testCase.request, invokeFunc(nil)); err == nil || !strings.Contains(err.Error(), testCase.want) {
			t.Fatalf("%s: error = %v, want %q", testCase.name, err, testCase.want)
		}
	}
}

func TestLocalExecutorScriptSyntaxErrorIsReported(t *testing.T) {
	result := runScript(t, "if True\n    pass\n", nil, DefaultLimits(), invokeFunc(nil))
	if result.Status != RunStatusFailed || result.Error == nil || result.Error.Type != "SyntaxError" {
		t.Fatalf("result = %+v", result)
	}
}

func assertProcessesGone(t *testing.T, pidFile string) {
	t.Helper()
	data, err := os.ReadFile(pidFile)
	if err != nil {
		t.Fatalf("read pid file: %v", err)
	}
	fields := strings.Fields(string(data))
	if len(fields) != 2 {
		t.Fatalf("pid file = %q", data)
	}
	if runtime.GOOS == "windows" {
		return
	}
	deadline := time.Now().Add(5 * time.Second)
	for _, field := range fields {
		pid, _ := strconv.Atoi(field)
		for processAlive(pid) {
			if time.Now().After(deadline) {
				t.Fatalf("process %d is still running after the run ended", pid)
			}
			time.Sleep(50 * time.Millisecond)
		}
	}
}
