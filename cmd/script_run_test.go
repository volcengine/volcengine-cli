package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/volcengine/volcengine-cli/internal/pyruntime"
	"github.com/volcengine/volcengine-cli/internal/safety"
	"github.com/volcengine/volcengine-cli/internal/scriptrun"
)

// scriptTestPython returns a real CPython for script integration tests.
// lookPathPython3 is resolved when the test binary starts, before tests
// replace PATH to control local interpreter discovery.
var lookPathPython3, lookPathPython3Err = exec.LookPath("python3")

func scriptTestPython(t *testing.T) string {
	t.Helper()
	path := os.Getenv("VOLCENGINE_TEST_PYTHON")
	if path == "" {
		if lookPathPython3Err != nil {
			t.Skip("python3 is not available; set VOLCENGINE_TEST_PYTHON")
		}
		path = lookPathPython3
	}
	absolute, err := filepath.Abs(path)
	if err != nil {
		t.Fatal(err)
	}
	return absolute
}

type scriptAPIRequest struct {
	Action     string
	Query      map[string]string
	AccessKey  string
	Region     string
	Token      string
	Authorized bool
}

// scriptMockAPI is a Volcengine OpenAPI mock that records signed requests.
type scriptMockAPI struct {
	t        *testing.T
	server   *httptest.Server
	mu       sync.Mutex
	requests []scriptAPIRequest
	hook     func(index int, request scriptAPIRequest)
}

func newScriptMockAPI(t *testing.T) *scriptMockAPI {
	mock := &scriptMockAPI{t: t}
	mock.server = httptest.NewServer(http.HandlerFunc(mock.serve))
	t.Cleanup(mock.server.Close)
	return mock
}

func (m *scriptMockAPI) serve(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	request := scriptAPIRequest{Action: r.Form.Get("Action"), Query: map[string]string{}, Token: r.Header.Get("X-Security-Token")}
	for key := range r.Form {
		request.Query[key] = r.Form.Get(key)
	}
	authorization := r.Header.Get("Authorization")
	if start := strings.Index(authorization, "Credential="); start >= 0 {
		scope := strings.Split(strings.SplitN(authorization[start+len("Credential="):], ",", 2)[0], "/")
		if len(scope) >= 3 {
			request.AccessKey, request.Region, request.Authorized = scope[0], scope[2], true
		}
	}
	m.mu.Lock()
	index := len(m.requests)
	m.requests = append(m.requests, request)
	hook := m.hook
	m.mu.Unlock()
	if hook != nil {
		hook(index, request)
	}

	requestID := fmt.Sprintf("req-%d", index+1)
	metadata := map[string]interface{}{"RequestId": requestID, "Action": request.Action, "Version": "2020-04-01", "Service": "ecs", "Region": request.Region}
	w.Header().Set("Content-Type", "application/json")
	var body map[string]interface{}
	switch request.Action {
	case "DescribeInstances":
		offset, _ := strconv.Atoi(request.Query["NextToken"])
		instances := []interface{}{}
		for id := offset; id < offset+2 && id < 5; id++ {
			instances = append(instances, map[string]interface{}{"InstanceId": fmt.Sprintf("i-%d", id), "InstanceTypeId": fmt.Sprintf("ecs.g%d", id%2)})
		}
		next := ""
		if offset+2 < 5 {
			next = strconv.Itoa(offset + 2)
		}
		body = map[string]interface{}{"ResponseMetadata": metadata, "Result": map[string]interface{}{"Instances": instances, "NextToken": next}}
	case "DescribeInstanceTypes":
		typeID := request.Query["InstanceTypeIds.1"]
		cores := 2
		if typeID == "ecs.g1" {
			cores = 8
		}
		body = map[string]interface{}{"ResponseMetadata": metadata, "Result": map[string]interface{}{"InstanceTypes": []interface{}{map[string]interface{}{"InstanceTypeId": typeID, "Processor": map[string]interface{}{"Cpus": cores}}}}}
	case "DescribeVpcs":
		metadata["Error"] = map[string]interface{}{"Code": "InvalidVpc.NotFound", "Message": "the VPC does not exist"}
		w.WriteHeader(http.StatusNotFound)
		body = map[string]interface{}{"ResponseMetadata": metadata}
	default:
		body = map[string]interface{}{"ResponseMetadata": metadata, "Result": map[string]interface{}{}}
	}
	_ = json.NewEncoder(w).Encode(body)
}

func (m *scriptMockAPI) snapshot() []scriptAPIRequest {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]scriptAPIRequest(nil), m.requests...)
}

// setupScriptProfile writes an AK profile pointing at the mock API and makes
// both the CLI and the SDK CLI credential provider read it.
func setupScriptProfile(t *testing.T, endpoint string) (string, func(*Profile)) {
	t.Helper()
	dir, cleanup := withTestConfigDir(t)
	t.Cleanup(cleanup)
	for _, name := range []string{"VOLCENGINE_ACCESS_KEY", "VOLCENGINE_SECRET_KEY", "VOLCENGINE_SESSION_TOKEN", "VOLCENGINE_PROFILE", "VOLCSTACK_PROFILE", "VOLCENGINE_REGION", "VOLCENGINE_ENDPOINT"} {
		t.Cleanup(unsetenvForTest(t, name))
	}
	t.Cleanup(setenvForTest(t, "VOLCENGINE_CLI_CONFIG_FILE", filepath.Join(dir, "config.json")))
	profile := &Profile{Name: "dev", Mode: ModeAK, AccessKey: "ak-1", SecretKey: "sk-1", SessionToken: "token-1", Region: "cn-beijing", Endpoint: endpoint}
	write := func(update *Profile) {
		if err := WriteConfigToFile(&Configure{Current: "dev", Profiles: map[string]*Profile{"dev": update}}); err != nil {
			t.Fatalf("write config: %v", err)
		}
	}
	write(profile)
	return dir, write
}

func runScriptForTest(t *testing.T, code string, limits scriptrun.Limits) *scriptRunReport {
	t.Helper()
	invoker, err := newReloadingVolcengineMCPInvoker(LoadConfig(), "", "")
	if err != nil {
		t.Fatalf("create invoker: %v", err)
	}
	interpreter, err := resolveScriptPython(context.Background(), scriptTestPython(t))
	if err != nil {
		t.Fatalf("resolve interpreter: %v", err)
	}
	runner := &localScriptRunner{invoker: invoker, interpreter: interpreter}
	report, err := runner.RunScript(context.Background(), scriptrun.Request{Code: code, Limits: limits})
	if err != nil {
		t.Fatalf("RunScript() error = %v", err)
	}
	return report
}

func TestScriptRunOrchestratesRealAPICallsThroughTheCLIPath(t *testing.T) {
	mock := newScriptMockAPI(t)
	dir, _ := setupScriptProfile(t, mock.server.URL)
	if err := safety.SavePolicy(dir, &safety.Policy{Enabled: true, Rules: []safety.Rule{
		{Pattern: "ecs:DeleteInstance", Action: safety.ActionConfirm},
		{Pattern: "ecs:StopInstance*", Action: safety.ActionDeny},
	}}); err != nil {
		t.Fatal(err)
	}
	// The environment-wide skip switch must not approve script calls.
	t.Cleanup(setenvForTest(t, safety.EnvSkipConfirm, "true"))

	code := `
import asyncio
instances, token = [], ""
while True:
    page = await call_api("ecs", "DescribeInstances", {"MaxResults": 2, "NextToken": token})
    instances += page["Result"]["Instances"]
    token = page["Result"]["NextToken"]
    if not token:
        break

types = sorted({item["InstanceTypeId"] for item in instances})
details = await asyncio.gather(*[call_api("ecs", "DescribeInstanceTypes", {"InstanceTypeIds.1": t}) for t in types])
cpus = {d["Result"]["InstanceTypes"][0]["InstanceTypeId"]: d["Result"]["InstanceTypes"][0]["Processor"]["Cpus"] for d in details}
big = [item["InstanceId"] for item in instances if cpus[item["InstanceTypeId"]] >= 8]

outcomes = {}
for service, api, params in [("ecs", "DeleteInstance", {"InstanceId": big[0]}), ("ecs", "StopInstances", {"InstanceIds.1": big[0]}), ("vpc", "DescribeVpcs", {"VpcIds.1": "vpc-x"}), ("ecs", "NoSuchApi", {})]:
    try:
        await call_api(service, api, params)
        outcomes[api] = "ok"
    except APIError as error:
        outcomes[api] = [error.code, error.status, error.request_id]

shanghai = await call_api("ecs", "DescribeInstances", {"MaxResults": 1}, region="cn-shanghai")
result = {"total": len(instances), "big": big, "outcomes": outcomes, "shanghai": len(shanghai["Result"]["Instances"])}
`
	report := runScriptForTest(t, code, scriptrun.DefaultLimits())
	if report.Status != scriptrun.RunStatusSucceeded {
		t.Fatalf("status = %s, error = %+v, output = %q", report.Status, report.Error, report.Output)
	}
	var got struct {
		Total    int                        `json:"total"`
		Big      []string                   `json:"big"`
		Outcomes map[string]json.RawMessage `json:"outcomes"`
	}
	if err := json.Unmarshal(report.Result.Result, &got); err != nil {
		t.Fatal(err)
	}
	if got.Total != 5 || strings.Join(got.Big, ",") != "i-1,i-3" {
		t.Fatalf("result = %s", report.Result.Result)
	}
	wantOutcomes := map[string]string{
		"DeleteInstance": `["SafetyConfirmationRequired","rejected",""]`,
		"StopInstances":  `["SafetyDenied","rejected",""]`,
		"DescribeVpcs":   `["InvalidVpc.NotFound","failed","req-`,
		"NoSuchApi":      `["UnsupportedAPI","rejected",""]`,
	}
	for api, want := range wantOutcomes {
		if !strings.HasPrefix(string(got.Outcomes[api]), want) {
			t.Fatalf("%s outcome = %s, want prefix %s", api, got.Outcomes[api], want)
		}
	}
	if report.Binding.Profile != "dev" || report.Binding.CredentialSource != mcpCredentialSourceProfile || report.Binding.Region != "cn-beijing" || report.Runtime == nil || report.Runtime.Source != "explicit" || report.Trust == "" {
		t.Fatalf("report metadata = %+v / %+v", report.Binding, report.Runtime)
	}

	requests := mock.snapshot()
	actions := []string{}
	for _, request := range requests {
		actions = append(actions, request.Action)
		if !request.Authorized || request.AccessKey != "ak-1" || request.Token != "token-1" {
			t.Fatalf("request was not signed with the bound profile: %+v", request)
		}
		if request.Action == "DeleteInstance" || request.Action == "StopInstances" {
			t.Fatalf("protected operation reached the API: %+v", request)
		}
	}
	if len(requests) != 3+2+1+1 || requests[len(requests)-1].Region != "cn-shanghai" || requests[0].Region != "cn-beijing" {
		t.Fatalf("API requests = %v (%+v)", actions, requests)
	}
	if report.Summary.OK != 6 || report.Summary.Rejected != 3 || report.Summary.Failed != 1 {
		t.Fatalf("summary = %+v, journal = %+v", report.Summary, report.Calls)
	}
	if report.Calls[0].RequestID != "req-1" || report.Calls[0].Status != scriptrun.CallStatusOK {
		t.Fatalf("successful journal entries must keep the RequestId: %+v", report.Calls[0])
	}
	for _, entry := range report.Calls {
		if entry.API == "DeleteInstance" && !strings.Contains(entry.Error, "call_api") {
			t.Fatalf("confirmation journal entry does not direct the agent to call_api: %+v", entry)
		}
	}
}

func TestScriptRunBindsIdentityAndRegionButAllowsCredentialRefresh(t *testing.T) {
	mock := newScriptMockAPI(t)
	_, write := setupScriptProfile(t, mock.server.URL)
	mock.hook = func(index int, _ scriptAPIRequest) {
		switch index {
		case 0:
			// Same identity refreshes its session credentials and the profile
			// region changes; the run keeps its bound default region.
			write(&Profile{Name: "dev", Mode: ModeAK, AccessKey: "ak-1", SecretKey: "sk-1", SessionToken: "token-2", Region: "cn-shanghai", Endpoint: mock.server.URL})
		case 1:
			// A different access key is a different identity.
			write(&Profile{Name: "dev", Mode: ModeAK, AccessKey: "ak-2", SecretKey: "sk-2", Region: "cn-beijing", Endpoint: mock.server.URL})
		}
	}
	code := `
first = await call_api("ecs", "DescribeInstances", {"MaxResults": 1})
second = await call_api("ecs", "DescribeInstances", {"MaxResults": 1})
try:
    await call_api("ecs", "DescribeInstances", {"MaxResults": 1})
except APIError as error:
    pass
await call_api("ecs", "DescribeInstances", {"MaxResults": 1})
result = "unreachable"
`
	report := runScriptForTest(t, code, scriptrun.DefaultLimits())
	if report.Status != scriptrun.RunStatusAborted || report.Error == nil || !strings.Contains(report.Error.Message, scriptErrorIdentityChanged) {
		t.Fatalf("status = %s, error = %+v", report.Status, report.Error)
	}
	requests := mock.snapshot()
	if len(requests) != 2 {
		t.Fatalf("requests after the identity change reached the API: %+v", requests)
	}
	if requests[0].Token != "token-1" || requests[1].Token != "token-2" || requests[1].AccessKey != "ak-1" || requests[1].Region != "cn-beijing" {
		t.Fatalf("refresh/binding requests = %+v", requests)
	}
	if len(report.Calls) != 3 || report.Calls[2].ErrorCode != scriptErrorIdentityChanged || report.Calls[2].Status != scriptrun.CallStatusRejected {
		t.Fatalf("journal = %+v", report.Calls)
	}
}

func TestScriptRunAppliesSafetyPolicyChangesToEveryRequest(t *testing.T) {
	mock := newScriptMockAPI(t)
	dir, _ := setupScriptProfile(t, mock.server.URL)
	mock.hook = func(index int, _ scriptAPIRequest) {
		if index == 0 {
			if err := safety.SavePolicy(dir, &safety.Policy{Enabled: true, Rules: []safety.Rule{{Pattern: "ecs:Describe*", Action: safety.ActionDeny}}}); err != nil {
				t.Errorf("save policy: %v", err)
			}
		}
	}
	code := `
await call_api("ecs", "DescribeInstances", {"MaxResults": 1})
try:
    await call_api("ecs", "DescribeInstances", {"MaxResults": 1})
    result = "allowed"
except APIError as error:
    result = error.code
`
	report := runScriptForTest(t, code, scriptrun.DefaultLimits())
	if report.Status != scriptrun.RunStatusSucceeded || string(report.Result.Result) != `"SafetyDenied"` || len(mock.snapshot()) != 1 {
		t.Fatalf("status = %s, result = %s, requests = %d", report.Status, report.Result.Result, len(mock.snapshot()))
	}
}

func TestScriptRunRequiresAPreparedRuntime(t *testing.T) {
	setupScriptProfile(t, "http://127.0.0.1:1")
	t.Cleanup(unsetenvForTest(t, scriptPythonEnvironment))
	// No local Python on PATH and no ve-managed runtime.
	t.Cleanup(setenvForTest(t, "PATH", t.TempDir()))
	if _, err := resolveScriptPython(context.Background(), ""); err == nil || !strings.Contains(err.Error(), "ve script runtime install") || !strings.Contains(err.Error(), "PATH") {
		t.Fatalf("unprepared runtime error = %v", err)
	}
	invoker, err := newReloadingVolcengineMCPInvoker(LoadConfig(), "", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := newMCPToolDependencies(context.Background(), mcpCommandOptions{Transport: mcpTransportStdio, EnableRunScript: true}, invoker); err == nil || !strings.Contains(err.Error(), "ve script runtime install") {
		t.Fatalf("MCP startup error = %v", err)
	}
	runner := &localScriptRunner{invoker: invoker}
	if _, err := runner.RunScript(context.Background(), scriptrun.Request{Code: "result = 1", Limits: scriptrun.DefaultLimits()}); err == nil {
		t.Fatal("a runner without a selected interpreter ran a script")
	}
}

func TestRunScriptIsOnlyEnabledForStdio(t *testing.T) {
	setupScriptProfile(t, "http://127.0.0.1:1")
	invoker, err := newReloadingVolcengineMCPInvoker(LoadConfig(), "", "")
	if err != nil {
		t.Fatal(err)
	}
	for _, transportName := range []string{mcpTransportSSE, mcpTransportStreamableHTTP} {
		_, err := newMCPToolDependencies(context.Background(), mcpCommandOptions{Transport: transportName, EnableRunScript: true, ScriptPython: scriptTestPython(t)}, invoker)
		if err == nil || !strings.Contains(err.Error(), "stdio") {
			t.Fatalf("%s: error = %v", transportName, err)
		}
	}
	dependencies, err := newMCPToolDependencies(context.Background(), mcpCommandOptions{Transport: mcpTransportStdio, EnableRunScript: true, ScriptPython: scriptTestPython(t)}, invoker)
	if err != nil || dependencies.scriptRunner == nil {
		t.Fatalf("stdio dependencies = %+v, %v", dependencies, err)
	}
	dependencies, err = newMCPToolDependencies(context.Background(), mcpCommandOptions{Transport: mcpTransportStdio}, invoker)
	if err != nil || dependencies.scriptRunner != nil || dependencies.search == nil {
		t.Fatalf("default dependencies = %+v, %v", dependencies, err)
	}
	if _, err := newMCPToolDependencies(context.Background(), mcpCommandOptions{Transport: mcpTransportStdio, EnableRunScript: true, ScriptPython: "/nonexistent/python3"}, invoker); err == nil {
		t.Fatal("an invalid interpreter was accepted at startup")
	}
}

func TestScriptLimitsFromArguments(t *testing.T) {
	limits, err := scriptLimitsFromArguments(map[string]interface{}{"max_calls": float64(5), "max_concurrency": 2, "timeout_seconds": float64(30), "call_timeout_seconds": float64(3)})
	if err != nil || limits.MaxCalls != 5 || limits.MaxConcurrency != 2 || limits.Timeout.Seconds() != 30 || limits.CallTimeout.Seconds() != 3 {
		t.Fatalf("limits = %+v, %v", limits, err)
	}
	for _, arguments := range []map[string]interface{}{{"max_calls": 0}, {"max_calls": 100000}, {"max_concurrency": 99}, {"timeout_seconds": 1.5}, {"call_timeout_seconds": "3"}} {
		if _, err := scriptLimitsFromArguments(arguments); err == nil {
			t.Fatalf("limits %v were accepted", arguments)
		}
	}
}

// Changing the role target of a role-assuming profile is a different
// principal even when RoleTrn is empty and AccountId/RoleName select the
// role. Invoke must reject it before any request is sent.
func TestScriptBindingRejectsRoleTargetChangeBeforeSending(t *testing.T) {
	mock := newScriptMockAPI(t)
	_, cleanup := withTestConfigDir(t)
	defer cleanup()
	profile := &Profile{Name: "default", Mode: ModeRamRoleArn, AccessKey: "base-ak", SecretKey: "base-sk", Region: "cn-beijing", AccountId: "account-a", RoleName: "role-a", Endpoint: mock.server.URL}
	cfg := &Configure{Current: "default", Profiles: map[string]*Profile{"default": profile}}
	base, err := newVolcengineMCPInvoker(cfg, "default", "")
	if err != nil {
		t.Fatal(err)
	}
	bound, err := newScriptAPIInvoker(base, mcpRequestMetadata{})
	if err != nil {
		t.Fatal(err)
	}
	for _, change := range []func(*Profile){
		func(p *Profile) { p.AccountId = "account-b" },
		func(p *Profile) { p.RoleName = "role-b" },
		func(p *Profile) { p.RoleTrn = "trn:iam::account-c:role/role-c" },
		func(p *Profile) { p.AccessKey = "other-base-ak" },
	} {
		original := *profile
		change(profile)
		_, callErr := bound.Invoke(context.Background(), scriptrun.Call{Service: "ecs", API: "DescribeInstances", Parameters: map[string]interface{}{}})
		if callErr == nil || callErr.Code != scriptErrorIdentityChanged || !callErr.Fatal || callErr.Status != scriptrun.CallStatusRejected {
			t.Fatalf("changed profile %+v: error = %+v", profile, callErr)
		}
		*profile = original
	}
	if requests := mock.snapshot(); len(requests) != 0 {
		t.Fatalf("requests reached the API: %+v", requests)
	}
}

// Default-chain temporary credentials cannot be tied to a principal locally,
// so a rotated access key fails closed instead of being trusted by provider
// name.
func TestScriptBindingFailsClosedWhenDefaultChainCredentialsRotate(t *testing.T) {
	mock := newScriptMockAPI(t)
	_, cleanup := withTestConfigDir(t)
	defer cleanup()
	for _, name := range []string{"VOLCENGINE_PROFILE", "VOLCSTACK_PROFILE", "VOLCENGINE_DISABLE_DEFAULT_CREDENTIALS"} {
		defer unsetenvForTest(t, name)()
	}
	defer setenvForTest(t, "VOLCENGINE_CLI_CONFIG_FILE", filepath.Join(t.TempDir(), "missing.json"))()
	defer setenvForTest(t, "VOLCENGINE_ACCESS_KEY", "temp-ak-1")()
	defer setenvForTest(t, "VOLCENGINE_SECRET_KEY", "temp-sk-1")()
	defer setenvForTest(t, "VOLCENGINE_SESSION_TOKEN", "temp-token-1")()
	defer setenvForTest(t, "VOLCENGINE_REGION", "cn-beijing")()
	defer setenvForTest(t, "VOLCENGINE_ENDPOINT", mock.server.URL)()
	mock.hook = func(index int, _ scriptAPIRequest) {
		if index == 0 {
			_ = os.Setenv("VOLCENGINE_ACCESS_KEY", "temp-ak-2")
			_ = os.Setenv("VOLCENGINE_SESSION_TOKEN", "temp-token-2")
		}
	}
	code := `
await call_api("ecs", "DescribeInstances", {"MaxResults": 1})
await call_api("ecs", "DescribeInstances", {"MaxResults": 1})
result = "unreachable"
`
	report := runScriptForTest(t, code, scriptrun.DefaultLimits())
	if report.Status != scriptrun.RunStatusAborted || report.Binding.CredentialSource != mcpCredentialSourceDefaultChain || len(mock.snapshot()) != 1 {
		t.Fatalf("status = %s, binding = %+v, requests = %d", report.Status, report.Binding, len(mock.snapshot()))
	}
}

// The SDK writes its transport and proxy into the configured http.Client
// for every client. Sharing http.DefaultClient makes concurrent API calls
// (concurrent call_api requests or run_script calls) race, so each CLI SDK
// client must own its HTTP client.
func TestSDKClientsDoNotShareTheDefaultHTTPClient(t *testing.T) {
	defer setenvForTest(t, "VOLCENGINE_ACCESS_KEY", "ak")()
	defer setenvForTest(t, "VOLCENGINE_SECRET_KEY", "sk")()
	defer setenvForTest(t, "VOLCENGINE_REGION", "cn-beijing")()
	first, err := NewSimpleClient(NewContext())
	if err != nil {
		t.Fatal(err)
	}
	second, err := NewSimpleClient(NewContext())
	if err != nil {
		t.Fatal(err)
	}
	if first.Config.HTTPClient == nil || first.Config.HTTPClient == http.DefaultClient || first.Config.HTTPClient == second.Config.HTTPClient {
		t.Fatalf("SDK clients share an HTTP client: %p %p (default %p)", first.Config.HTTPClient, second.Config.HTTPClient, http.DefaultClient)
	}
}

// loggingPython writes a python3 launcher for the real test interpreter that
// records whether it was started as a validation probe or as a script worker.
func loggingPython(t *testing.T, dir string) (string, string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	log := filepath.Join(dir, "invocations.log")
	launcher := filepath.Join(dir, "python3")
	body := "#!/bin/sh\ncase \"$*\" in *python_version*) echo probe >> '" + log + "';; *) echo worker >> '" + log + "';; esac\nexec '" + scriptTestPython(t) + "' \"$@\"\n"
	if err := os.WriteFile(launcher, []byte(body), 0755); err != nil {
		t.Fatal(err)
	}
	return launcher, log
}

func countLines(t *testing.T, path, want string) int {
	t.Helper()
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return 0
	}
	if err != nil {
		t.Fatal(err)
	}
	count := 0
	for _, line := range strings.Split(string(data), "\n") {
		if line == want {
			count++
		}
	}
	return count
}

func TestMCPRunScriptSelectsLocalPythonOnceAtStartup(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the logging launcher is a POSIX shell script")
	}
	mock := newScriptMockAPI(t)
	setupScriptProfile(t, mock.server.URL)
	t.Cleanup(unsetenvForTest(t, scriptPythonEnvironment))
	launcher, log := loggingPython(t, filepath.Join(t.TempDir(), "local python"))
	t.Cleanup(setenvForTest(t, "PATH", filepath.Dir(launcher)))

	invoker, err := newReloadingVolcengineMCPInvoker(LoadConfig(), "", "")
	if err != nil {
		t.Fatal(err)
	}
	dependencies, err := newMCPToolDependencies(context.Background(), mcpCommandOptions{Transport: mcpTransportStdio, EnableRunScript: true}, invoker)
	if err != nil {
		t.Fatalf("startup: %v", err)
	}
	if probes := countLines(t, log, "probe"); probes != 1 {
		t.Fatalf("startup probed %d times, want 1", probes)
	}

	// Later PATH changes do not switch the selected interpreter, and runs do
	// not repeat discovery or validation.
	other, otherLog := loggingPython(t, filepath.Join(t.TempDir(), "other python"))
	t.Cleanup(setenvForTest(t, "PATH", filepath.Dir(other)))
	for run := 0; run < 2; run++ {
		report, err := dependencies.scriptRunner.RunScript(context.Background(), scriptrun.Request{Code: "result = 1", Limits: scriptrun.DefaultLimits()})
		if err != nil || report.Status != scriptrun.RunStatusSucceeded {
			t.Fatalf("run %d = %+v, %v", run, report, err)
		}
		if report.Runtime.Path != launcher || report.Runtime.Source != pyruntime.SourceSystem {
			t.Fatalf("run %d runtime = %+v", run, report.Runtime)
		}
	}
	if probes, workers := countLines(t, log, "probe"), countLines(t, log, "worker"); probes != 1 || workers != 2 {
		t.Fatalf("probes = %d, workers = %d; want 1 and 2", probes, workers)
	}
	if countLines(t, otherLog, "probe")+countLines(t, otherLog, "worker") != 0 {
		t.Fatal("a later PATH interpreter was used")
	}
}

func TestMCPWithoutRunScriptDoesNotDetectPython(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the logging launcher is a POSIX shell script")
	}
	setupScriptProfile(t, "http://127.0.0.1:1")
	launcher, log := loggingPython(t, filepath.Join(t.TempDir(), "bin"))
	t.Cleanup(setenvForTest(t, "PATH", filepath.Dir(launcher)))
	invoker, err := newReloadingVolcengineMCPInvoker(LoadConfig(), "", "")
	if err != nil {
		t.Fatal(err)
	}
	dependencies, err := newMCPToolDependencies(context.Background(), mcpCommandOptions{Transport: mcpTransportStdio}, invoker)
	if err != nil || dependencies.scriptRunner != nil {
		t.Fatalf("dependencies = %+v, %v", dependencies, err)
	}
	if _, err := os.Stat(log); !os.IsNotExist(err) {
		t.Fatalf("Python was started although run_script is disabled: %v", err)
	}
}

func TestScriptRunnerReportsMissingSelectedInterpreter(t *testing.T) {
	setupScriptProfile(t, "http://127.0.0.1:1")
	invoker, err := newReloadingVolcengineMCPInvoker(LoadConfig(), "", "")
	if err != nil {
		t.Fatal(err)
	}
	missing := filepath.Join(t.TempDir(), "removed", "python3")
	runner := &localScriptRunner{invoker: invoker, interpreter: &pyruntime.Interpreter{Path: missing, Source: pyruntime.SourceSystem}}
	_, err = runner.RunScript(context.Background(), scriptrun.Request{Code: "result = 1", Limits: scriptrun.DefaultLimits()})
	if err == nil || !strings.Contains(err.Error(), missing) || !strings.Contains(err.Error(), "no longer available") {
		t.Fatalf("missing interpreter error = %v", err)
	}
}

func TestResolveScriptPythonReportsSourceAndHonorsExplicitOverrides(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the logging launcher is a POSIX shell script")
	}
	setupScriptProfile(t, "http://127.0.0.1:1")
	launcher, _ := loggingPython(t, filepath.Join(t.TempDir(), "path python"))
	t.Cleanup(setenvForTest(t, "PATH", filepath.Dir(launcher)))
	t.Cleanup(unsetenvForTest(t, scriptPythonEnvironment))

	interpreter, err := resolveScriptPython(context.Background(), "")
	if err != nil || interpreter.Source != pyruntime.SourceSystem || interpreter.Path != launcher {
		t.Fatalf("local = %+v, %v", interpreter, err)
	}
	explicit := scriptTestPython(t)
	t.Cleanup(setenvForTest(t, scriptPythonEnvironment, explicit))
	interpreter, err = resolveScriptPython(context.Background(), "")
	if err != nil || interpreter.Source != pyruntime.SourceExplicit || interpreter.Path != explicit {
		t.Fatalf("environment override = %+v, %v", interpreter, err)
	}
	// The flag wins over the environment, and an invalid explicit path never
	// falls back to the local interpreter.
	interpreter, err = resolveScriptPython(context.Background(), launcher)
	if err != nil || interpreter.Source != pyruntime.SourceExplicit || interpreter.Path != launcher {
		t.Fatalf("flag override = %+v, %v", interpreter, err)
	}
	if _, err := resolveScriptPython(context.Background(), filepath.Join(t.TempDir(), "missing")); err == nil || strings.Contains(err.Error(), "not prepared") {
		t.Fatalf("invalid flag error = %v", err)
	}
}
