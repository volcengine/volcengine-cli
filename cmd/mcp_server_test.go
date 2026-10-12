package cmd

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	mcpclient "github.com/ThinkInAIXYZ/go-mcp/client"
	"github.com/ThinkInAIXYZ/go-mcp/protocol"
	"github.com/ThinkInAIXYZ/go-mcp/transport"
	"github.com/golang-jwt/jwt/v5"
	"github.com/volcengine/volcengine-cli/internal/docs"
	"github.com/volcengine/volcengine-cli/internal/safety"
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
	registerMCPTools(registrar, rootSupport, stubMCPInvoker{}, &fakeDocsModule{})

	want := []string{"list_services", "list_apis", "get_api_parameters", "call_api", "search_docs", "fetch_docs"}
	for _, name := range want {
		if registrar.tools[name] == nil {
			t.Fatalf("tool %q was not registered", name)
		}
	}
}

func TestMCPListToolsExposeEnumerationSchemas(t *testing.T) {
	registrar := &recordingMCPRegistrar{}
	registerMCPTools(registrar, rootSupport, stubMCPInvoker{}, &fakeDocsModule{})

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
	var approval struct {
		Type        string `json:"type"`
		Description string `json:"description"`
	}
	if err := json.Unmarshal(callSchema.Properties["yes"], &approval); err != nil {
		t.Fatal(err)
	}
	if approval.Type != "boolean" || approval.Description != safety.MCPApprovalDescription {
		t.Fatalf("yes schema does not use the shared approval description: %#v", approval)
	}
	for _, required := range callSchema.Required {
		if required == "region" || required == "yes" {
			t.Fatalf("call_api region and yes must be optional: %s", registrar.tools["call_api"].RawInputSchema)
		}
	}
}

func TestMCPCallAPISchemaEscapesDescription(t *testing.T) {
	want := "Ask \"continue?\"\nKeep path C:\\example unchanged."
	var schema struct {
		Properties map[string]struct {
			Description string `json:"description"`
		} `json:"properties"`
	}
	if err := json.Unmarshal(mcpCallAPIInputSchema(want), &schema); err != nil {
		t.Fatal(err)
	}
	if got := schema.Properties["yes"].Description; got != want {
		t.Fatalf("description round-trip = %q, want %q", got, want)
	}
}

func TestMCPCallAPIForwardsExplicitSafetyApproval(t *testing.T) {
	calls := make(chan recordedMCPCall, 1)
	registrar := &recordingMCPRegistrar{}
	registerMCPTools(registrar, rootSupport, recordingMCPInvoker{calls: calls}, &fakeDocsModule{})

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
	registerMCPTools(registrar, rootSupport, stubMCPInvoker{}, &fakeDocsModule{})
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
	registerMCPTools(registrar, rootSupport, recordingMCPInvoker{calls: calls}, &fakeDocsModule{})
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

func TestMCPSearchDocsDefaultsSchemaAndAnnotations(t *testing.T) {
	docsModule := &fakeDocsModule{searchResult: docs.SearchResult{
		RequestID: "request-id",
		Documents: []docs.SearchDocument{{Title: "TOS quick start", URL: "https://www.volcengine.com/docs/6349/74830"}},
	}}
	registrar := &recordingMCPRegistrar{}
	registerMCPTools(registrar, rootSupport, stubMCPInvoker{}, docsModule)

	tool := registrar.tools["search_docs"]
	if tool == nil {
		t.Fatal("search_docs was not registered")
	}
	var schema struct {
		Properties map[string]json.RawMessage `json:"properties"`
		Required   []string                   `json:"required"`
	}
	if err := json.Unmarshal(tool.RawInputSchema, &schema); err != nil {
		t.Fatalf("decode search_docs schema: %v", err)
	}
	for _, name := range []string{"query", "limit", "service_codes", "snippet_length"} {
		if _, ok := schema.Properties[name]; !ok {
			t.Errorf("search_docs schema missing %q: %s", name, tool.RawInputSchema)
		}
	}
	if fmt.Sprint(schema.Required) != "[query]" {
		t.Fatalf("search_docs required fields = %v, want [query]", schema.Required)
	}
	if annotations := tool.Annotations; annotations == nil || annotations.ReadOnlyHint == nil || !*annotations.ReadOnlyHint ||
		annotations.DestructiveHint == nil || *annotations.DestructiveHint ||
		annotations.IdempotentHint == nil || !*annotations.IdempotentHint ||
		annotations.OpenWorldHint == nil || !*annotations.OpenWorldHint {
		t.Fatalf("search_docs annotations = %#v", annotations)
	}

	result, err := registrar.handlers["search_docs"](context.Background(), &protocol.CallToolRequest{Arguments: map[string]interface{}{
		"query": "object storage quick start",
	}})
	if err != nil || result.IsError {
		t.Fatalf("search_docs result = %#v, error = %v", result, err)
	}
	wantOptions := docs.SearchOptions{
		Query:         "object storage quick start",
		Limit:         docs.DefaultSearchLimit,
		SnippetLength: docs.DefaultSnippetLength,
	}
	if !reflect.DeepEqual(docsModule.searchOptions, wantOptions) {
		t.Fatalf("SearchOptions = %#v, want %#v", docsModule.searchOptions, wantOptions)
	}
	if len(result.Content) != 1 || !strings.Contains(result.Content[0].(*protocol.TextContent).Text, `"request_id": "request-id"`) {
		t.Fatalf("search_docs content = %#v", result.Content)
	}
}

func TestMCPSearchDocsForwardsOptionsAndReturnsToolErrors(t *testing.T) {
	docsModule := &fakeDocsModule{}
	registrar := &recordingMCPRegistrar{}
	registerMCPTools(registrar, rootSupport, stubMCPInvoker{}, docsModule)
	handler := registrar.handlers["search_docs"]

	result, err := handler(context.Background(), &protocol.CallToolRequest{Arguments: map[string]interface{}{
		"query":          "TOS",
		"limit":          float64(3),
		"service_codes":  []interface{}{"tos", "billing"},
		"snippet_length": float64(240),
	}})
	if err != nil || result.IsError {
		t.Fatalf("search_docs result = %#v, error = %v", result, err)
	}
	wantOptions := docs.SearchOptions{Query: "TOS", Limit: 3, ServiceCodes: []string{"tos", "billing"}, SnippetLength: 240}
	if !reflect.DeepEqual(docsModule.searchOptions, wantOptions) {
		t.Fatalf("SearchOptions = %#v, want %#v", docsModule.searchOptions, wantOptions)
	}

	for _, test := range []struct {
		name      string
		arguments map[string]interface{}
		want      string
	}{
		{name: "missing query", arguments: map[string]interface{}{}, want: "query is required"},
		{name: "fractional limit", arguments: map[string]interface{}{"query": "q", "limit": 1.5}, want: "limit must be an integer"},
		{name: "non-positive limit", arguments: map[string]interface{}{"query": "q", "limit": float64(0)}, want: "limit must be a positive integer"},
		{name: "invalid service codes", arguments: map[string]interface{}{"query": "q", "service_codes": []interface{}{"tos", 1}}, want: "service_codes must be an array of strings"},
		{name: "non-positive snippet length", arguments: map[string]interface{}{"query": "q", "snippet_length": float64(-1)}, want: "snippet_length must be a positive integer"},
	} {
		t.Run(test.name, func(t *testing.T) {
			result, err := handler(context.Background(), &protocol.CallToolRequest{Arguments: test.arguments})
			if err != nil || !result.IsError {
				t.Fatalf("result = %#v, error = %v", result, err)
			}
			if len(result.Content) != 1 || !strings.Contains(result.Content[0].(*protocol.TextContent).Text, test.want) {
				t.Fatalf("content = %#v, want error containing %q", result.Content, test.want)
			}
		})
	}

	docsModule.searchErr = errors.New("documentation backend unavailable")
	result, err = handler(context.Background(), &protocol.CallToolRequest{Arguments: map[string]interface{}{"query": "q"}})
	if err != nil || !result.IsError || !strings.Contains(result.Content[0].(*protocol.TextContent).Text, docsModule.searchErr.Error()) {
		t.Fatalf("upstream error result = %#v, error = %v", result, err)
	}
}

func TestMCPFetchDocsDefaultsSchemaAndAnnotations(t *testing.T) {
	nextStartIndex := 5000
	docsModule := &fakeDocsModule{fetchResult: docs.FetchResult{
		RequestID:           "request-id",
		Title:               "TOS quick start",
		URL:                 "https://www.volcengine.com/docs/6349/74830",
		Content:             "document content",
		StartIndex:          0,
		NextStartIndex:      &nextStartIndex,
		RemainingCharacters: 120,
		HasMore:             true,
		HasContent:          true,
	}}
	registrar := &recordingMCPRegistrar{}
	registerMCPTools(registrar, rootSupport, stubMCPInvoker{}, docsModule)

	tool := registrar.tools["fetch_docs"]
	if tool == nil {
		t.Fatal("fetch_docs was not registered")
	}
	var schema struct {
		Properties map[string]json.RawMessage `json:"properties"`
		Required   []string                   `json:"required"`
	}
	if err := json.Unmarshal(tool.RawInputSchema, &schema); err != nil {
		t.Fatalf("decode fetch_docs schema: %v", err)
	}
	for _, name := range []string{"url", "start_index", "max_length"} {
		if _, ok := schema.Properties[name]; !ok {
			t.Errorf("fetch_docs schema missing %q: %s", name, tool.RawInputSchema)
		}
	}
	if fmt.Sprint(schema.Required) != "[url]" {
		t.Fatalf("fetch_docs required fields = %v, want [url]", schema.Required)
	}
	if annotations := tool.Annotations; annotations == nil || annotations.ReadOnlyHint == nil || !*annotations.ReadOnlyHint ||
		annotations.DestructiveHint == nil || *annotations.DestructiveHint ||
		annotations.IdempotentHint == nil || !*annotations.IdempotentHint ||
		annotations.OpenWorldHint == nil || !*annotations.OpenWorldHint {
		t.Fatalf("fetch_docs annotations = %#v", annotations)
	}

	result, err := registrar.handlers["fetch_docs"](context.Background(), &protocol.CallToolRequest{Arguments: map[string]interface{}{
		"url": "https://www.volcengine.com/docs/6349/74830",
	}})
	if err != nil || result.IsError {
		t.Fatalf("fetch_docs result = %#v, error = %v", result, err)
	}
	wantOptions := docs.FetchOptions{
		URL:       "https://www.volcengine.com/docs/6349/74830",
		MaxLength: docs.DefaultFetchLength,
	}
	if !reflect.DeepEqual(docsModule.fetchOptions, wantOptions) {
		t.Fatalf("FetchOptions = %#v, want %#v", docsModule.fetchOptions, wantOptions)
	}
	if len(result.Content) != 1 || !strings.Contains(result.Content[0].(*protocol.TextContent).Text, `"content": "document content"`) {
		t.Fatalf("fetch_docs content = %#v", result.Content)
	}
}

func TestMCPFetchDocsForwardsOptionsAndReturnsToolErrors(t *testing.T) {
	docsModule := &fakeDocsModule{}
	registrar := &recordingMCPRegistrar{}
	registerMCPTools(registrar, rootSupport, stubMCPInvoker{}, docsModule)
	handler := registrar.handlers["fetch_docs"]

	result, err := handler(context.Background(), &protocol.CallToolRequest{Arguments: map[string]interface{}{
		"url":         "https://www.volcengine.com/docs/6349/74830",
		"start_index": float64(120),
		"max_length":  float64(240),
	}})
	if err != nil || result.IsError {
		t.Fatalf("fetch_docs result = %#v, error = %v", result, err)
	}
	wantOptions := docs.FetchOptions{
		URL: "https://www.volcengine.com/docs/6349/74830", StartIndex: 120, MaxLength: 240,
	}
	if !reflect.DeepEqual(docsModule.fetchOptions, wantOptions) {
		t.Fatalf("FetchOptions = %#v, want %#v", docsModule.fetchOptions, wantOptions)
	}

	for _, test := range []struct {
		name      string
		arguments map[string]interface{}
		want      string
	}{
		{name: "missing URL", arguments: map[string]interface{}{}, want: "url is required"},
		{name: "fractional start index", arguments: map[string]interface{}{"url": "https://www.volcengine.com/docs/1/2", "start_index": 1.5}, want: "start_index must be an integer"},
		{name: "negative start index", arguments: map[string]interface{}{"url": "https://www.volcengine.com/docs/1/2", "start_index": float64(-1)}, want: "start_index must be a non-negative integer"},
		{name: "non-positive max length", arguments: map[string]interface{}{"url": "https://www.volcengine.com/docs/1/2", "max_length": float64(0)}, want: "max_length must be a positive integer"},
	} {
		t.Run(test.name, func(t *testing.T) {
			result, err := handler(context.Background(), &protocol.CallToolRequest{Arguments: test.arguments})
			if err != nil || !result.IsError {
				t.Fatalf("result = %#v, error = %v", result, err)
			}
			if len(result.Content) != 1 || !strings.Contains(result.Content[0].(*protocol.TextContent).Text, test.want) {
				t.Fatalf("content = %#v, want error containing %q", result.Content, test.want)
			}
		})
	}

	docsModule.fetchErr = errors.New("documentation backend unavailable")
	result, err = handler(context.Background(), &protocol.CallToolRequest{Arguments: map[string]interface{}{
		"url": "https://www.volcengine.com/docs/1/2",
	}})
	if err != nil || !result.IsError || !strings.Contains(result.Content[0].(*protocol.TextContent).Text, docsModule.fetchErr.Error()) {
		t.Fatalf("upstream error result = %#v, error = %v", result, err)
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
	want := []string{"call_api", "fetch_docs", "get_api_parameters", "list_apis", "list_services", "search_docs"}
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
