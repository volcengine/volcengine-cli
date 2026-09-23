package cmd

import (
	"crypto/rand"
	"encoding/hex"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/spf13/cobra"
	"github.com/volcengine/volcengine-cli/internal/clientid"
	"github.com/volcengine/volcengine-cli/internal/proxycli"
	"github.com/volcengine/volcengine-go-sdk/volcengine/credentials"
)

func newClientIDTestContext(t *testing.T, endpoint string) *Context {
	t.Helper()
	ctx := NewContext()
	ctx.config = &Configure{Profiles: map[string]*Profile{}}
	for name, value := range map[string]string{"endpoint": endpoint, "region": "cn-beijing"} {
		flag, err := ctx.fixedFlags.AddByName(name)
		if err != nil {
			t.Fatal(err)
		}
		flag.SetValue(value)
	}
	return ctx
}

func newClientIDTestSDK(t *testing.T, endpoint string) *SdkClient {
	t.Helper()
	// Use ephemeral credentials for the local signing fixture, never a real cloud key.
	var fixture [32]byte
	if _, err := rand.Read(fixture[:]); err != nil {
		t.Fatal(err)
	}
	sdk, err := newSimpleClient(newClientIDTestContext(t, endpoint), credentials.NewStaticCredentials(hex.EncodeToString(fixture[:16]), hex.EncodeToString(fixture[16:]), ""))
	if err != nil {
		t.Fatal(err)
	}
	return sdk
}

func TestSDKRequestsShareTheDocumentationClientID(t *testing.T) {
	directory := filepath.Join(t.TempDir(), "client")
	defer withConfigDirForTest(directory)()
	// Seed the same identity store used by docs search/fetch.
	want, err := clientid.LoadOrCreate(directory)
	if err != nil {
		t.Fatal(err)
	}
	var requests int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&requests, 1)
		if values := r.Header.Values(clientid.HeaderName); len(values) != 1 || values[0] != want {
			t.Errorf("client ID headers = %v, want [%s]", values, want)
		}
		if !strings.Contains(r.Header.Get("Authorization"), "x-client-id") {
			t.Error("client ID was not included before request signing")
		}
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Error(err)
		}
		if strings.Contains(strings.ToLower(r.URL.RawQuery+string(body)), "client-id") {
			t.Error("client ID leaked into API parameters")
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"ResponseMetadata":{"RequestId":"client-id-test"},"Result":{"Ok":true}}`)
	}))
	defer server.Close()

	cases := []struct {
		name, service, method, contentType string
		headers                            []requestHeader
	}{
		{name: "GET", service: "ecs", method: http.MethodGet},
		{name: "form POST", service: "ecs", method: http.MethodPost, contentType: "application/x-www-form-urlencoded"},
		{name: "JSON POST", service: "ecs", method: http.MethodPost, contentType: "application/json"},
		{name: "unlisted service", service: "clientid-unlisted", method: http.MethodPost, contentType: "application/json"},
		{name: "custom header cannot replace local identity", service: "ecs", method: http.MethodGet, headers: []requestHeader{{Name: "x-client-id", Value: "override"}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			sdk := newClientIDTestSDK(t, server.URL)
			input := map[string]interface{}{"Value": "test"}
			_, err := sdk.CallSdk(SdkClientInfo{
				ServiceName: tc.service, Action: "DescribeInstances", Version: "2020-01-01",
				Method: tc.method, ContentType: tc.contentType, Headers: tc.headers,
			}, &input)
			if err != nil {
				t.Fatal(err)
			}
		})
	}
	if got := atomic.LoadInt32(&requests); got != int32(len(cases)) {
		t.Fatalf("requests = %d, want %d", got, len(cases))
	}
}

func TestSDKClientIDIsCreatedOnlyForAValidRequest(t *testing.T) {
	directory := filepath.Join(t.TempDir(), "client")
	defer withConfigDirForTest(directory)()
	var received string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		received = r.Header.Get(clientid.HeaderName)
		_, _ = io.WriteString(w, `{"Result":{}}`)
	}))
	defer server.Close()
	sdk := newClientIDTestSDK(t, server.URL)
	info := SdkClientInfo{ServiceName: "ecs", Action: "DescribeInstances", Version: "2020-01-01", Method: http.MethodPost, ContentType: "application/json"}
	if _, err := os.Stat(directory); !os.IsNotExist(err) {
		t.Fatalf("client construction touched client ID storage: %v", err)
	}
	t.Setenv("VOLCENGINE_ACCESS_KEY", "test-ak")
	t.Setenv("VOLCENGINE_SECRET_KEY", "test-sk")
	ctx := newClientIDTestContext(t, server.URL)
	err := executeInvocation(ctx, invocationParams{
		serviceName: "ecs", action: "DescribeInstances", version: "2020-01-01",
		method: http.MethodPost, contentType: "application/json",
	}, func() (invocationInput, error) {
		value, err := parseJSONBody(`{"broken":`)
		return invocationInput{value: value, jsonBody: true, fromBody: true}, err
	})
	if err == nil || !strings.Contains(err.Error(), "json format error") {
		t.Fatalf("expected input validation failure, got %v", err)
	}
	if _, err := os.Stat(filepath.Join(directory, "client_id")); !os.IsNotExist(err) {
		t.Fatalf("invalid request touched client ID storage: %v", err)
	}
	if _, err := sdk.CallSdk(info, nil); err != nil {
		t.Fatal(err)
	}
	persisted, err := os.ReadFile(filepath.Join(directory, "client_id"))
	if err != nil || received == "" || received != strings.TrimSpace(string(persisted)) {
		t.Fatalf("received ID = %q, persisted = %q, error = %v", received, persisted, err)
	}
}

func TestSDKClientIDFailurePreventsRequest(t *testing.T) {
	directory := t.TempDir()
	defer withConfigDirForTest(directory)()
	file := filepath.Join(directory, "client_id")
	if err := os.WriteFile(file, []byte("invalid-id\n"), 0600); err != nil {
		t.Fatal(err)
	}
	var requests int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&requests, 1)
		_, _ = io.WriteString(w, `{"Result":{}}`)
	}))
	defer server.Close()
	sdk := newClientIDTestSDK(t, server.URL)
	_, err := sdk.CallSdk(SdkClientInfo{ServiceName: "ecs", Action: "DescribeInstances", Version: "2020-01-01", Method: http.MethodGet}, nil)
	if err == nil || !strings.Contains(err.Error(), "client ID") {
		t.Fatalf("expected client ID error, got %v", err)
	}
	if atomic.LoadInt32(&requests) != 0 {
		t.Fatal("sent a request without a stable client ID")
	}
	content, err := os.ReadFile(file)
	if err != nil || string(content) != "invalid-id\n" {
		t.Fatalf("invalid existing identity was replaced: %q, %v", content, err)
	}
}

func TestProxyDelegationDoesNotCreateClientID(t *testing.T) {
	directory := t.TempDir()
	defer withConfigDirForTest(directory)()
	oldRun := runProxyExecutable
	t.Cleanup(func() { runProxyExecutable = oldRun })
	runProxyExecutable = func(string, []string, io.Reader, io.Writer, io.Writer, []string) error { return nil }
	for _, tool := range []proxycli.Tool{proxycli.ToolArkCLI, proxycli.ToolTOSUtil} {
		manager := &fakeProxyManager{resolved: proxycli.Resolution{Tool: tool, Path: "/managed/" + string(tool)}}
		if err := runProxyCLI(&cobra.Command{}, nil, manager, tool, []string{"--help"}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := os.Stat(filepath.Join(directory, "client_id")); !os.IsNotExist(err) {
		t.Fatalf("proxy delegation touched client ID storage: %v", err)
	}
}
