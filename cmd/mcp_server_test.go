package cmd

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"sort"
	"strings"
	"testing"
	"time"

	mcpclient "github.com/ThinkInAIXYZ/go-mcp/client"
	"github.com/ThinkInAIXYZ/go-mcp/protocol"
	"github.com/ThinkInAIXYZ/go-mcp/transport"
	"github.com/golang-jwt/jwt/v5"
)

type recordingMCPRegistrar struct {
	tools    map[string]*protocol.Tool
	handlers map[string]mcpToolHandler
}

func (r *recordingMCPRegistrar) RegisterTool(tool *protocol.Tool, handler mcpToolHandler) {
	if r.tools == nil {
		r.tools = make(map[string]*protocol.Tool)
		r.handlers = make(map[string]mcpToolHandler)
	}
	r.tools[tool.Name] = tool
	r.handlers[tool.Name] = handler
}

type stubMCPInvoker struct{}

func (stubMCPInvoker) CallAPI(context.Context, string, string, string, map[string]interface{}, bool) (map[string]interface{}, error) {
	return map[string]interface{}{}, nil
}

type recordedMCPCall struct {
	metadata mcpRequestMetadata
	region   string
	yes      bool
}

type recordingMCPInvoker struct {
	calls chan recordedMCPCall
}

func (i recordingMCPInvoker) CallAPI(ctx context.Context, _, _, region string, _ map[string]interface{}, yes bool) (map[string]interface{}, error) {
	i.calls <- recordedMCPCall{metadata: mcpRequestMetadataFromContext(ctx), region: region, yes: yes}
	return map[string]interface{}{"ok": true}, nil
}

func TestRegisterMCPTools(t *testing.T) {
	registrar := &recordingMCPRegistrar{}
	registerMCPTools(registrar, rootSupport, stubMCPInvoker{})

	want := []string{"list_services", "list_apis", "get_api_parameters", "call_api"}
	for _, name := range want {
		if registrar.tools[name] == nil {
			t.Fatalf("tool %q was not registered", name)
		}
	}
}

func TestMCPListToolsExposeEnumerationSchemas(t *testing.T) {
	registrar := &recordingMCPRegistrar{}
	registerMCPTools(registrar, rootSupport, stubMCPInvoker{})

	servicesSchema := string(registrar.tools["list_services"].RawInputSchema)
	if strings.Contains(servicesSchema, "query") || strings.Contains(servicesSchema, "limit") {
		t.Fatalf("list_services schema still exposes search controls: %s", servicesSchema)
	}
	apisSchema := string(registrar.tools["list_apis"].RawInputSchema)
	if !strings.Contains(apisSchema, `"service"`) || strings.Contains(apisSchema, "query") || strings.Contains(apisSchema, "limit") {
		t.Fatalf("list_apis schema is not service-only: %s", apisSchema)
	}

	var callSchema struct {
		Properties map[string]json.RawMessage `json:"properties"`
		Required   []string                   `json:"required"`
	}
	if err := json.Unmarshal(registrar.tools["call_api"].RawInputSchema, &callSchema); err != nil {
		t.Fatalf("decode call_api schema: %v", err)
	}
	if _, ok := callSchema.Properties["region"]; !ok {
		t.Fatalf("call_api schema does not expose region: %s", registrar.tools["call_api"].RawInputSchema)
	}
	if _, ok := callSchema.Properties["yes"]; !ok {
		t.Fatalf("call_api schema does not expose yes: %s", registrar.tools["call_api"].RawInputSchema)
	}
	for _, phrase := range []string{"Omit on every new operation", "only after a block", "new user reply", "ONE unchanged retry", "internally", "even on failure", "Cannot bypass deny"} {
		if !strings.Contains(string(callSchema.Properties["yes"]), phrase) {
			t.Errorf("yes schema missing approval instruction %q", phrase)
		}
	}
	for _, required := range callSchema.Required {
		if required == "region" || required == "yes" {
			t.Fatalf("call_api region and yes must be optional: %s", registrar.tools["call_api"].RawInputSchema)
		}
	}
}

func TestMCPCallAPIForwardsExplicitSafetyApproval(t *testing.T) {
	calls := make(chan recordedMCPCall, 1)
	registrar := &recordingMCPRegistrar{}
	registerMCPTools(registrar, rootSupport, recordingMCPInvoker{calls: calls})

	result, err := registrar.handlers["call_api"](context.Background(), &protocol.CallToolRequest{Arguments: map[string]interface{}{
		"service": "sts", "api": "GetCallerIdentity", "yes": true,
	}})
	if err != nil || result.IsError {
		t.Fatalf("call_api result = %#v, error = %v", result, err)
	}
	if call := <-calls; !call.yes {
		t.Fatal("call_api did not forward explicit safety approval")
	}
}

func TestMCPCallAPIRejectsNonBooleanSafetyApproval(t *testing.T) {
	registrar := &recordingMCPRegistrar{}
	registerMCPTools(registrar, rootSupport, stubMCPInvoker{})
	result, err := registrar.handlers["call_api"](context.Background(), &protocol.CallToolRequest{Arguments: map[string]interface{}{
		"service": "sts", "api": "GetCallerIdentity", "yes": "true",
	}})
	if err != nil || !result.IsError {
		t.Fatalf("call_api result = %#v, error = %v", result, err)
	}
	text, ok := result.Content[0].(*protocol.TextContent)
	if !ok || !strings.Contains(text.Text, "yes must be a boolean") {
		t.Fatalf("call_api error content = %#v", result.Content)
	}
}

func TestMCPCallAPIRegionIsOptionalAndValidated(t *testing.T) {
	calls := make(chan recordedMCPCall, 2)
	registrar := &recordingMCPRegistrar{}
	registerMCPTools(registrar, rootSupport, recordingMCPInvoker{calls: calls})
	handler := registrar.handlers["call_api"]

	for _, test := range []struct {
		name      string
		arguments map[string]interface{}
		want      string
	}{
		{name: "omitted", arguments: map[string]interface{}{"service": "sts", "api": "GetCallerIdentity"}},
		{name: "provided", arguments: map[string]interface{}{"service": "sts", "api": "GetCallerIdentity", "region": "ap-southeast-1"}, want: "ap-southeast-1"},
	} {
		t.Run(test.name, func(t *testing.T) {
			result, err := handler(context.Background(), &protocol.CallToolRequest{Arguments: test.arguments})
			if err != nil || result.IsError {
				t.Fatalf("call_api result = %#v, error = %v", result, err)
			}
			if got := (<-calls).region; got != test.want {
				t.Fatalf("region = %q, want %q", got, test.want)
			}
		})
	}

	result, err := handler(context.Background(), &protocol.CallToolRequest{Arguments: map[string]interface{}{
		"service": "sts", "api": "GetCallerIdentity", "region": "invalid/region",
	}})
	if err != nil || !result.IsError {
		t.Fatalf("invalid region result = %#v, error = %v", result, err)
	}
}

func TestMCPStdioProtocol(t *testing.T) {
	if os.Getenv("VOLCENGINE_MCP_STDIO_HELPER") == "1" {
		if err := runMCPStdio(stubMCPInvoker{}); err != nil {
			t.Fatalf("runMCPStdio() error = %v", err)
		}
		return
	}

	clientTransport, err := transport.NewStdioClientTransport(
		os.Args[0], []string{"-test.run=^TestMCPStdioProtocol$"},
		transport.WithStdioClientOptionEnv("VOLCENGINE_MCP_STDIO_HELPER=1"),
	)
	if err != nil {
		t.Fatalf("NewStdioClientTransport() error = %v", err)
	}
	client, err := mcpclient.NewClient(clientTransport, mcpclient.WithInitTimeout(10*time.Second))
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}
	assertMCPClientTools(t, client)
	if err := client.Close(); err != nil {
		t.Fatalf("client.Close() error = %v", err)
	}
}

func TestMCPSSEProtocol(t *testing.T) {
	testMCPHTTPProtocol(t, mcpTransportSSE)
}

func TestMCPStreamableHTTPProtocol(t *testing.T) {
	testMCPHTTPProtocol(t, mcpTransportStreamableHTTP)
}

func TestMCPSSEOAuthProtocol(t *testing.T) {
	testMCPHTTPOAuthProtocol(t, mcpTransportSSE)
}

func TestMCPStreamableHTTPOAuthProtocol(t *testing.T) {
	testMCPHTTPOAuthProtocol(t, mcpTransportStreamableHTTP)
}

func testMCPHTTPOAuthProtocol(t *testing.T, transportName string) {
	t.Helper()
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate RSA key: %v", err)
	}
	var issuer string
	authorizationServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/.well-known/oauth-authorization-server":
			_ = json.NewEncoder(w).Encode(map[string]interface{}{"issuer": issuer, "jwks_uri": issuer + "/jwks"})
		case "/jwks":
			_ = json.NewEncoder(w).Encode(map[string]interface{}{"keys": []interface{}{map[string]interface{}{
				"kty": "RSA", "kid": "oauth-key", "use": "sig",
				"n": base64.RawURLEncoding.EncodeToString(privateKey.PublicKey.N.Bytes()),
				"e": base64.RawURLEncoding.EncodeToString(big.NewInt(int64(privateKey.PublicKey.E)).Bytes()),
			}}})
		default:
			http.NotFound(w, r)
		}
	}))
	defer authorizationServer.Close()
	issuer = authorizationServer.URL

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	baseURL := "http://" + listener.Addr().String()
	ctx, cancel := context.WithCancel(context.Background())
	serverDone := make(chan error, 1)
	go func() {
		serverDone <- runMCPHTTP(ctx, listener, baseURL, mcpCommandOptions{
			Transport: transportName, OAuth: true,
			OAuthIssuer: issuer, OAuthAudience: "volcengine-mcp", OAuthScopes: []string{"mcp:use"},
		}, stubMCPInvoker{})
	}()

	endpoint := "/mcp"
	method := http.MethodPost
	if transportName == mcpTransportSSE {
		endpoint = "/sse"
		method = http.MethodGet
	}
	metadataURL := baseURL + "/.well-known/oauth-protected-resource" + endpoint
	waitForMCPHTTPStatus(t, metadataURL, http.StatusOK)
	request, _ := http.NewRequest(method, baseURL+endpoint, strings.NewReader(`{}`))
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		cancel()
		t.Fatalf("unauthenticated request: %v", err)
	}
	_ = response.Body.Close()
	if response.StatusCode != http.StatusUnauthorized || !strings.Contains(response.Header.Get("WWW-Authenticate"), "resource_metadata=") {
		cancel()
		t.Fatalf("unauthenticated status = %d, challenge = %q", response.StatusCode, response.Header.Get("WWW-Authenticate"))
	}

	token := jwt.NewWithClaims(jwt.SigningMethodRS256, mcpJWTClaims{
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer: issuer, Subject: "oauth-user", Audience: jwt.ClaimStrings{"volcengine-mcp"},
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
		},
		Scope: "mcp:use",
	})
	token.Header["kid"] = "oauth-key"
	signed, err := token.SignedString(privateKey)
	if err != nil {
		cancel()
		t.Fatalf("sign token: %v", err)
	}
	var clientTransport transport.ClientTransport
	if transportName == mcpTransportSSE {
		httpClient := &http.Client{Transport: mcpHeaderRoundTripper{headers: map[string]string{"Authorization": "Bearer " + signed}}}
		clientTransport, err = transport.NewSSEClientTransport(baseURL+endpoint, transport.WithSSEClientOptionHTTPClient(httpClient))
	} else {
		clientTransport, err = transport.NewStreamableHTTPClientTransport(
			baseURL+endpoint,
			transport.WithStreamableHTTPClientOptionHeader(map[string]string{"Authorization": "Bearer " + signed}),
		)
	}
	if err != nil {
		cancel()
		t.Fatalf("create client transport: %v", err)
	}
	client, err := mcpclient.NewClient(clientTransport, mcpclient.WithInitTimeout(10*time.Second))
	if err != nil {
		cancel()
		t.Fatalf("NewClient() error = %v", err)
	}
	assertMCPClientTools(t, client)
	if err := client.Close(); err != nil {
		t.Fatalf("client.Close() error = %v", err)
	}
	cancel()
	select {
	case err := <-serverDone:
		if err != nil {
			t.Fatalf("runMCPHTTP() error = %v", err)
		}
	case <-time.After(15 * time.Second):
		t.Fatal("timed out waiting for OAuth MCP server shutdown")
	}
}

func testMCPHTTPProtocol(t *testing.T, transportName string) {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	baseURL := "http://" + listener.Addr().String() + "/prefix"
	ctx, cancel := context.WithCancel(context.Background())
	serverDone := make(chan error, 1)
	calls := make(chan recordedMCPCall, 1)
	go func() {
		serverDone <- runMCPHTTP(ctx, listener, baseURL, mcpCommandOptions{Transport: transportName}, recordingMCPInvoker{calls: calls})
	}()

	headers := map[string]string{mcpCredentialsHeader: testMCPCredentialHeader()}
	httpClient := &http.Client{Transport: mcpHeaderRoundTripper{headers: headers}}
	var clientTransport transport.ClientTransport
	switch transportName {
	case mcpTransportSSE:
		clientTransport, err = transport.NewSSEClientTransport(baseURL+"/sse", transport.WithSSEClientOptionHTTPClient(httpClient))
	case mcpTransportStreamableHTTP:
		clientTransport, err = transport.NewStreamableHTTPClientTransport(baseURL+"/mcp", transport.WithStreamableHTTPClientOptionHeader(headers))
	default:
		t.Fatalf("unexpected transport %q", transportName)
	}
	if err != nil {
		cancel()
		t.Fatalf("create client transport: %v", err)
	}
	client, err := mcpclient.NewClient(clientTransport, mcpclient.WithInitTimeout(10*time.Second))
	if err != nil {
		cancel()
		t.Fatalf("NewClient() error = %v", err)
	}
	assertMCPClientTools(t, client)
	result, err := client.CallTool(context.Background(), &protocol.CallToolRequest{
		Name: "call_api", Arguments: map[string]interface{}{"service": "sts", "api": "GetCallerIdentity", "region": "ap-southeast-1", "parameters": map[string]interface{}{}},
	})
	if err != nil || result.IsError {
		t.Fatalf("CallTool() result = %#v, error = %v", result, err)
	}
	select {
	case got := <-calls:
		if got.region != "ap-southeast-1" || got.metadata.Credentials == nil || got.metadata.Credentials.AccessKeyID != "ak" {
			t.Fatalf("recorded call = %#v", got)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for call_api metadata")
	}
	if err := client.Close(); err != nil {
		t.Fatalf("client.Close() error = %v", err)
	}
	cancel()
	select {
	case err := <-serverDone:
		if err != nil {
			t.Fatalf("runMCPHTTP() error = %v", err)
		}
	case <-time.After(15 * time.Second):
		t.Fatal("timed out waiting for MCP server shutdown")
	}
}

func assertMCPClientTools(t *testing.T, client *mcpclient.Client) {
	t.Helper()
	result, err := client.ListTools(context.Background())
	if err != nil {
		t.Fatalf("ListTools() error = %v", err)
	}
	names := make([]string, 0, len(result.Tools))
	for _, tool := range result.Tools {
		names = append(names, tool.Name)
	}
	sort.Strings(names)
	want := []string{"call_api", "get_api_parameters", "list_apis", "list_services"}
	if fmt.Sprint(names) != fmt.Sprint(want) {
		t.Fatalf("tools = %v, want %v", names, want)
	}
	call, err := client.CallTool(context.Background(), &protocol.CallToolRequest{Name: "list_services", Arguments: map[string]interface{}{}})
	if err != nil || call.IsError {
		t.Fatalf("list_services result = %#v, error = %v", call, err)
	}
}

type mcpHeaderRoundTripper struct {
	headers map[string]string
}

func (t mcpHeaderRoundTripper) RoundTrip(request *http.Request) (*http.Response, error) {
	clone := request.Clone(request.Context())
	for name, value := range t.headers {
		clone.Header.Set(name, value)
	}
	return http.DefaultTransport.RoundTrip(clone)
}

func testMCPCredentialHeader() string {
	return "eyJBY2Nlc3NLZXlJZCI6ImFrIiwiU2VjcmV0QWNjZXNzS2V5Ijoic2siLCJTZXNzaW9uVG9rZW4iOiJ0b2tlbiJ9"
}

func waitForMCPHTTPStatus(t *testing.T, endpoint string, status int) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		response, err := http.Get(endpoint)
		if err == nil {
			_ = response.Body.Close()
			if response.StatusCode == status {
				return
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("%s did not return HTTP %d", endpoint, status)
}
