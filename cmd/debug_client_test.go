package cmd

import (
	"bytes"
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
