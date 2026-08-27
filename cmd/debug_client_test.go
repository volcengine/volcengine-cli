package cmd

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestNewSimpleClientWritesCliDebugSummary(t *testing.T) {
	disableSSL := false
	ctx := NewContext()
	ctx.config = &Configure{
		Current: "default",
		Profiles: map[string]*Profile{
			"default": {
				Name:       "default",
				Mode:       ModeAK,
				AccessKey:  "ak-should-not-leak",
				SecretKey:  "sk-should-not-leak",
				Region:     "cn-beijing",
				Endpoint:   "sts.volcengineapi.com",
				DisableSSL: &disableSSL,
			},
		},
	}
	var out bytes.Buffer
	ctx.debugLogger = &DebugLogger{enabled: true, out: &out}

	if _, err := NewSimpleClient(ctx); err != nil {
		t.Fatalf("NewSimpleClient returned error: %v", err)
	}

	logs := out.String()
	for _, want := range []string{
		"profile_source=current",
		"profile=default",
		"credential_mode=ak",
		"region=cn-beijing",
		"endpoint=sts.volcengineapi.com",
	} {
		if !strings.Contains(logs, want) {
			t.Fatalf("debug logs missing %q:\n%s", want, logs)
		}
	}
	if strings.Contains(logs, "ak-should-not-leak") || strings.Contains(logs, "sk-should-not-leak") {
		t.Fatalf("debug logs leaked credentials:\n%s", logs)
	}
}

func TestNewSimpleClientUsesProfileEndpointWhenNoFlag(t *testing.T) {
	disableSSL := false
	ctx := NewContext()
	ctx.config = &Configure{
		Current: "default",
		Profiles: map[string]*Profile{
			"default": {
				Name:       "default",
				Mode:       ModeAK,
				AccessKey:  "ak-test",
				SecretKey:  "sk-test",
				Region:     "cn-beijing",
				Endpoint:   "sts.volcengineapi.com",
				DisableSSL: &disableSSL,
			},
		},
	}

	var out bytes.Buffer
	ctx.debugLogger = &DebugLogger{enabled: true, out: &out}

	sdk, err := NewSimpleClient(ctx)
	if err != nil {
		t.Fatalf("NewSimpleClient returned error: %v", err)
	}
	if sdk.Config.Endpoint == nil || *sdk.Config.Endpoint != "sts.volcengineapi.com" {
		got := ""
		if sdk.Config.Endpoint != nil {
			got = *sdk.Config.Endpoint
		}
		t.Fatalf("expected profile endpoint, got %q", got)
	}

	logs := out.String()
	if !strings.Contains(logs, "endpoint=sts.volcengineapi.com") {
		t.Fatalf("debug logs should reflect profile endpoint, got:\n%s", logs)
	}
}

func TestCallSdkAppliesCustomHeadersAndJSONContentType(t *testing.T) {
	var gotCT, gotFoo string
	const contentType = "application/json; profile=readme; charset=utf-8"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotCT = r.Header.Get("Content-Type")
		gotFoo = r.Header.Get("X-Foo")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ResponseMetadata":{"RequestId":"req-header-1","Action":"DescribeInstances","Version":"2020-01-01","Service":"ecs","Region":"cn-beijing"},"Result":{"Ok":true}}`))
	}))
	defer server.Close()

	defer setenvForTest(t, "VOLCENGINE_ACCESS_KEY", "ak-test")()
	defer setenvForTest(t, "VOLCENGINE_SECRET_KEY", "sk-test")()
	defer setenvForTest(t, "VOLCENGINE_REGION", "cn-beijing")()

	ctx := NewContext()
	endpointFlag, err := ctx.fixedFlags.AddByName("endpoint")
	if err != nil {
		t.Fatalf("add endpoint flag: %v", err)
	}
	endpointFlag.SetValue(server.URL)

	sdk, err := NewSimpleClient(ctx)
	if err != nil {
		t.Fatalf("NewSimpleClient: %v", err)
	}
	if _, err := sdk.CallSdk(SdkClientInfo{
		ServiceName: "ecs",
		Action:      "DescribeInstances",
		Version:     "2020-01-01",
		Method:      "POST",
		ContentType: contentType,
		Headers: []requestHeader{
			{Name: "X-Foo", Value: "bar"},
			{Name: "Content-Type", Value: contentType},
		},
	}, &map[string]interface{}{"k": "v"}); err != nil {
		t.Fatalf("CallSdk: %v", err)
	}
	if gotFoo != "bar" {
		t.Fatalf("X-Foo = %q, want bar", gotFoo)
	}
	if gotCT != contentType {
		t.Fatalf("Content-Type = %q, want exact user value %q", gotCT, contentType)
	}
}

func TestCallSdkPreservesLargeJSONInteger(t *testing.T) {
	const wantBody = `{"Id":9223372036854775807}`
	var gotBody string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("read request body: %v", err)
		}
		gotBody = string(body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ResponseMetadata":{"RequestId":"req-json-number","Action":"GetAccountSummary","Version":"2018-01-01","Service":"iam","Region":"cn-beijing"},"Result":{"Id":9223372036854775807}}`))
	}))
	defer server.Close()

	defer setenvForTest(t, "VOLCENGINE_ACCESS_KEY", "ak-test")()
	defer setenvForTest(t, "VOLCENGINE_SECRET_KEY", "sk-test")()
	defer setenvForTest(t, "VOLCENGINE_REGION", "cn-beijing")()

	ctx := NewContext()
	endpointFlag, err := ctx.fixedFlags.AddByName("endpoint")
	if err != nil {
		t.Fatalf("add endpoint flag: %v", err)
	}
	endpointFlag.SetValue(server.URL)

	sdk, err := NewSimpleClient(ctx)
	if err != nil {
		t.Fatalf("NewSimpleClient: %v", err)
	}
	input, err := parseJSONBody(wantBody)
	if err != nil {
		t.Fatalf("parseJSONBody: %v", err)
	}
	output, err := sdk.CallSdk(SdkClientInfo{
		ServiceName: "iam",
		Action:      "GetAccountSummary",
		Version:     "2018-01-01",
		Method:      "POST",
		ContentType: "application/json",
	}, input)
	if err != nil {
		t.Fatalf("CallSdk: %v", err)
	}
	if gotBody != wantBody {
		t.Fatalf("request body = %q, want %q", gotBody, wantBody)
	}
	result, ok := (*output)["Result"].(map[string]interface{})
	if !ok {
		t.Fatalf("Result = %#v, want map", (*output)["Result"])
	}
	if got, ok := result["Id"].(json.Number); !ok || got.String() != "9223372036854775807" {
		t.Fatalf("response Result.Id = %#v, want exact json.Number", result["Id"])
	}
}

func TestCallSdkWritesDebugRequestAttemptWithRequestID(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ResponseMetadata":{"RequestId":"req-debug-123","Action":"DescribeInstances","Version":"2020-01-01","Service":"ecs","Region":"cn-beijing"},"Result":{"Ok":true}}`))
	}))
	defer server.Close()

	defer setenvForTest(t, "VOLCENGINE_ACCESS_KEY", "ak-test")()
	defer setenvForTest(t, "VOLCENGINE_SECRET_KEY", "sk-test")()
	defer setenvForTest(t, "VOLCENGINE_REGION", "cn-beijing")()

	ctx := NewContext()
	endpointFlag, err := ctx.fixedFlags.AddByName("endpoint")
	if err != nil {
		t.Fatalf("add endpoint flag: %v", err)
	}
	endpointFlag.SetValue(server.URL)

	var out bytes.Buffer
	ctx.debugLogger = &DebugLogger{enabled: true, out: &out}

	sdk, err := NewSimpleClient(ctx)
	if err != nil {
		t.Fatalf("NewSimpleClient returned error: %v", err)
	}
	if _, err := sdk.CallSdk(SdkClientInfo{
		ServiceName: "ecs",
		Action:      "DescribeInstances",
		Version:     "2020-01-01",
		Method:      "GET",
	}, &map[string]interface{}{}); err != nil {
		t.Fatalf("CallSdk returned error: %v", err)
	}

	logs := out.String()
	for _, want := range []string{
		"sdk_request_attempt",
		"service=ecs",
		"action=DescribeInstances",
		"status_code=200",
		"request_id=req-debug-123",
		"retry_count=0",
	} {
		if !strings.Contains(logs, want) {
			t.Fatalf("debug logs missing %q:\n%s", want, logs)
		}
	}
}

func TestCallSdkWritesDebugRequestAttemptErrorWithRequestID(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"ResponseMetadata":{"RequestId":"req-error-456","Error":{"Code":"InvalidParameter","Message":"bad input"}}}`))
	}))
	defer server.Close()

	defer setenvForTest(t, "VOLCENGINE_ACCESS_KEY", "ak-test")()
	defer setenvForTest(t, "VOLCENGINE_SECRET_KEY", "sk-test")()
	defer setenvForTest(t, "VOLCENGINE_REGION", "cn-beijing")()

	ctx := NewContext()
	endpointFlag, err := ctx.fixedFlags.AddByName("endpoint")
	if err != nil {
		t.Fatalf("add endpoint flag: %v", err)
	}
	endpointFlag.SetValue(server.URL)

	var out bytes.Buffer
	ctx.debugLogger = &DebugLogger{enabled: true, out: &out}

	sdk, err := NewSimpleClient(ctx)
	if err != nil {
		t.Fatalf("NewSimpleClient returned error: %v", err)
	}
	if _, err := sdk.CallSdk(SdkClientInfo{
		ServiceName: "ecs",
		Action:      "DescribeInstances",
		Version:     "2020-01-01",
		Method:      "GET",
	}, &map[string]interface{}{}); err == nil {
		t.Fatal("expected CallSdk to return service error")
	}

	logs := out.String()
	for _, want := range []string{
		"sdk_request_attempt",
		"status_code=400",
		"request_id=req-error-456",
		"error=InvalidParameter",
	} {
		if !strings.Contains(logs, want) {
			t.Fatalf("debug logs missing %q:\n%s", want, logs)
		}
	}
}
