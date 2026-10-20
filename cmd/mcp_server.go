package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"math"

	"github.com/ThinkInAIXYZ/go-mcp/protocol"
	"github.com/ThinkInAIXYZ/go-mcp/server"
	"github.com/volcengine/volcengine-cli/internal/apisearch"
	"github.com/volcengine/volcengine-cli/internal/docs"
	"github.com/volcengine/volcengine-cli/internal/safety"
)

type mcpToolHandler = server.ToolHandlerFunc

type mcpToolRegistrar interface {
	RegisterTool(*protocol.Tool, mcpToolHandler)
}

type mcpServerRegistrar struct {
	server *server.Server
}

func (r mcpServerRegistrar) RegisterTool(tool *protocol.Tool, handler mcpToolHandler) {
	r.server.RegisterTool(tool, handler)
}

type mcpAPIInvoker interface {
	CallAPI(context.Context, string, string, string, map[string]interface{}, bool) (map[string]interface{}, error)
}

func mcpCallAPIInputSchema(approvalDescription string) json.RawMessage {
	type property struct {
		Type                 string `json:"type"`
		Description          string `json:"description,omitempty"`
		Pattern              string `json:"pattern,omitempty"`
		AdditionalProperties bool   `json:"additionalProperties,omitempty"`
	}
	schema := struct {
		Type       string              `json:"type"`
		Properties map[string]property `json:"properties"`
		Required   []string            `json:"required"`
	}{
		Type: "object",
		Properties: map[string]property{
			"service":    {Type: "string"},
			"api":        {Type: "string"},
			"region":     {Type: "string", Description: "Override the region for this API call.", Pattern: "^[A-Za-z0-9][A-Za-z0-9-]{0,127}$"},
			"parameters": {Type: "object", AdditionalProperties: true},
			"yes":        {Type: "boolean", Description: approvalDescription},
		},
		Required: []string{"service", "api"},
	}
	// This closed type contains only strings, booleans, maps and slices, so
	// marshaling cannot fail. Descriptions are escaped by encoding/json.
	data, _ := json.Marshal(schema)
	return data
}

type mcpDocsModule interface {
	Search(context.Context, docs.SearchOptions) (docs.SearchResult, error)
	Fetch(context.Context, docs.FetchOptions) (docs.FetchResult, error)
}

type mcpAPISearchModule interface {
	Search(context.Context, apisearch.SearchOptions) (apisearch.SearchResult, error)
}

// mcpAPISearchItem is one search result with only the official identity,
// FullContent and upstream score, to save tokens. Parameters come from
// get_api_parameters.
type mcpAPISearchItem struct {
	Action      string  `json:"action"`
	Service     string  `json:"service"`
	Version     string  `json:"version"`
	FullContent string  `json:"FullContent"`
	Score       float64 `json:"score"`
}

type mcpAPISearchResult struct {
	Total      int                `json:"Total"`
	NextOffset *int               `json:"NextOffset,omitempty"`
	APIs       []mcpAPISearchItem `json:"APIs"`
}

const mcpDiscoveryFallbackHint = "fall back to list_services and list_apis"

func newMCPAPISearchResult(result apisearch.SearchResult) mcpAPISearchResult {
	compact := mcpAPISearchResult{Total: result.Total, NextOffset: result.NextOffset, APIs: make([]mcpAPISearchItem, 0, len(result.APIs))}
	for _, record := range result.APIs {
		compact.APIs = append(compact.APIs, mcpAPISearchItem{Action: record.Action, Service: record.ServiceCode, Version: record.Version, FullContent: record.FullContent, Score: record.Score})
	}
	return compact
}

// registerMCPTools registers the MCP tools. scriptRunner is nil unless
// trusted local script execution was explicitly enabled.
func registerMCPTools(registrar mcpToolRegistrar, support *RootSupport, invoker mcpAPIInvoker, docsModule mcpDocsModule, searchModule mcpAPISearchModule, scriptRunner mcpScriptRunner) {
	searchAPITool := protocol.NewToolWithRawSchema("search_api", "Start API discovery here. Searches the official public Volcano Engine API catalog (no credentials needed) and returns, per result, only action, service, version, FullContent and score as published by the catalog. Get the parameters this CLI sends with get_api_parameters; call_api uses the CLI's built-in version of the service. Use NextOffset as offset for more results. If the search fails or finds nothing useful, fall back to list_services and list_apis.", json.RawMessage(`{"type":"object","properties":{"query":{"type":"string","description":"Keywords or a natural-language task.","minLength":1,"maxLength":1000},"limit":{"type":"integer","description":"Page size.","minimum":1,"maximum":50,"default":10},"offset":{"type":"integer","description":"Number of records to skip; use NextOffset from the previous page.","minimum":0,"default":0}},"required":["query"],"additionalProperties":false}`))
	searchAPITool.Annotations = mcpExternalReadOnlyToolAnnotations()
	registrar.RegisterTool(searchAPITool, func(ctx context.Context, request *protocol.CallToolRequest) (*protocol.CallToolResult, error) {
		if searchModule == nil {
			return mcpErrorResult(fmt.Errorf("API search is not available")), nil
		}
		query, err := mcpStringArgument(request.Arguments, "query", true)
		if err != nil {
			return mcpErrorResult(err), nil
		}
		limit, err := mcpPositiveIntArgument(request.Arguments, "limit", apisearch.DefaultLimit)
		if err != nil {
			return mcpErrorResult(err), nil
		}
		offset, err := mcpNonNegativeIntArgument(request.Arguments, "offset", 0)
		if err != nil {
			return mcpErrorResult(err), nil
		}
		result, err := searchModule.Search(ctx, apisearch.SearchOptions{Query: query, Limit: limit, Offset: offset})
		if err != nil {
			return mcpErrorResult(fmt.Errorf("%w; %s", err, mcpDiscoveryFallbackHint)), nil
		}
		return mcpCompactJSONResult(newMCPAPISearchResult(result))
	})

	listServicesTool := protocol.NewToolWithRawSchema("list_services", "Fallback discovery when search_api fails or finds nothing useful: list every Volcano Engine service code supported by this CLI. Use the returned service code with list_apis.", json.RawMessage(`{"type":"object","properties":{}}`))
	listServicesTool.Annotations = mcpReadOnlyToolAnnotations()
	registrar.RegisterTool(listServicesTool, func(_ context.Context, _ *protocol.CallToolRequest) (*protocol.CallToolResult, error) {
		return mcpJSONResult(listMCPServices(support))
	})

	listAPIsTool := protocol.NewToolWithRawSchema("list_apis", "Fallback discovery when search_api fails or finds nothing useful: list every supported API action for one Volcano Engine service code returned by list_services.", json.RawMessage(`{"type":"object","properties":{"service":{"type":"string"}},"required":["service"]}`))
	listAPIsTool.Annotations = mcpReadOnlyToolAnnotations()
	registrar.RegisterTool(listAPIsTool, func(_ context.Context, request *protocol.CallToolRequest) (*protocol.CallToolResult, error) {
		serviceName, err := mcpStringArgument(request.Arguments, "service", true)
		if err != nil {
			return mcpErrorResult(err), nil
		}
		result, err := listMCPAPIs(support, serviceName)
		if err != nil {
			return mcpErrorResult(err), nil
		}
		return mcpJSONResult(result)
	})

	getParametersTool := protocol.NewToolWithRawSchema("get_api_parameters", "Get request parameters, types, required fields, and an example for a Volcano Engine API.", json.RawMessage(`{"type":"object","properties":{"service":{"type":"string"},"api":{"type":"string"}},"required":["service","api"]}`))
	getParametersTool.Annotations = mcpReadOnlyToolAnnotations()
	registrar.RegisterTool(getParametersTool, func(_ context.Context, request *protocol.CallToolRequest) (*protocol.CallToolResult, error) {
		serviceName, err := mcpStringArgument(request.Arguments, "service", true)
		if err != nil {
			return mcpErrorResult(err), nil
		}
		api, err := mcpStringArgument(request.Arguments, "api", true)
		if err != nil {
			return mcpErrorResult(err), nil
		}
		result, err := getMCPAPIParameters(support, serviceName, api)
		if err != nil {
			return mcpErrorResult(err), nil
		}
		return mcpJSONResult(result)
	})

	callAPITool := protocol.NewToolWithRawSchema("call_api", "Call any supported Volcano Engine API. Cloud IAM permissions and the local safety policy determine whether the call is allowed.", mcpCallAPIInputSchema(safety.MCPApprovalDescription))
	readOnly, destructive, idempotent, openWorld := false, true, false, true
	callAPITool.Annotations = &protocol.ToolAnnotations{
		ReadOnlyHint: &readOnly, DestructiveHint: &destructive, IdempotentHint: &idempotent, OpenWorldHint: &openWorld,
	}
	registrar.RegisterTool(callAPITool, func(ctx context.Context, request *protocol.CallToolRequest) (*protocol.CallToolResult, error) {
		serviceName, err := mcpStringArgument(request.Arguments, "service", true)
		if err != nil {
			return mcpErrorResult(err), nil
		}
		api, err := mcpStringArgument(request.Arguments, "api", true)
		if err != nil {
			return mcpErrorResult(err), nil
		}
		region, err := mcpStringArgument(request.Arguments, "region", false)
		if err != nil {
			return mcpErrorResult(err), nil
		}
		region, err = normalizeMCPRegion(region)
		if err != nil {
			return mcpErrorResult(err), nil
		}
		parameters, err := mcpObjectArgument(request.Arguments, "parameters")
		if err != nil {
			return mcpErrorResult(err), nil
		}
		yes, err := mcpBoolArgument(request.Arguments, "yes")
		if err != nil {
			return mcpErrorResult(err), nil
		}
		result, err := invoker.CallAPI(ctx, serviceName, api, region, parameters, yes)
		if err != nil {
			return mcpErrorResult(err), nil
		}
		return mcpJSONResult(result)
	})

	if scriptRunner != nil {
		registerMCPRunScriptTool(registrar, scriptRunner)
	}

	searchDocsTool := protocol.NewToolWithRawSchema("search_docs", "Search public Volcano Engine documentation and return matching titles, URLs, service codes, and text snippets.", json.RawMessage(`{"type":"object","properties":{"query":{"type":"string","description":"Text to search for.","minLength":1},"limit":{"type":"integer","description":"Maximum number of documents to return.","minimum":1,"default":10},"service_codes":{"type":"array","description":"Optional service codes used to filter results.","items":{"type":"string"}},"snippet_length":{"type":"integer","description":"Maximum number of Unicode characters per result snippet.","minimum":1,"default":600}},"required":["query"],"additionalProperties":false}`))
	searchDocsTool.Annotations = mcpExternalReadOnlyToolAnnotations()
	registrar.RegisterTool(searchDocsTool, func(ctx context.Context, request *protocol.CallToolRequest) (*protocol.CallToolResult, error) {
		if docsModule == nil {
			return mcpErrorResult(fmt.Errorf("documentation search is not configured")), nil
		}
		query, err := mcpStringArgument(request.Arguments, "query", true)
		if err != nil {
			return mcpErrorResult(err), nil
		}
		limit, err := mcpPositiveIntArgument(request.Arguments, "limit", docs.DefaultSearchLimit)
		if err != nil {
			return mcpErrorResult(err), nil
		}
		serviceCodes, err := mcpStringArrayArgument(request.Arguments, "service_codes")
		if err != nil {
			return mcpErrorResult(err), nil
		}
		snippetLength, err := mcpPositiveIntArgument(request.Arguments, "snippet_length", docs.DefaultSnippetLength)
		if err != nil {
			return mcpErrorResult(err), nil
		}
		result, err := docsModule.Search(ctx, docs.SearchOptions{
			Query: query, Limit: limit, ServiceCodes: serviceCodes, SnippetLength: snippetLength,
		})
		if err != nil {
			return mcpErrorResult(err), nil
		}
		return mcpJSONResult(result)
	})

	fetchDocsTool := protocol.NewToolWithRawSchema("fetch_docs", "Fetch a public Volcano Engine documentation article and return a Unicode-safe page of its cleaned text content.", json.RawMessage(`{"type":"object","properties":{"url":{"type":"string","description":"Volcano Engine documentation URL returned by search_docs.","minLength":1},"start_index":{"type":"integer","description":"Zero-based Unicode character index at which to start.","minimum":0,"default":0},"max_length":{"type":"integer","description":"Maximum number of Unicode characters to return.","minimum":1,"default":5000}},"required":["url"],"additionalProperties":false}`))
	fetchDocsTool.Annotations = mcpExternalReadOnlyToolAnnotations()
	registrar.RegisterTool(fetchDocsTool, func(ctx context.Context, request *protocol.CallToolRequest) (*protocol.CallToolResult, error) {
		if docsModule == nil {
			return mcpErrorResult(fmt.Errorf("documentation fetch is not configured")), nil
		}
		url, err := mcpStringArgument(request.Arguments, "url", true)
		if err != nil {
			return mcpErrorResult(err), nil
		}
		startIndex, err := mcpNonNegativeIntArgument(request.Arguments, "start_index", 0)
		if err != nil {
			return mcpErrorResult(err), nil
		}
		maxLength, err := mcpPositiveIntArgument(request.Arguments, "max_length", docs.DefaultFetchLength)
		if err != nil {
			return mcpErrorResult(err), nil
		}
		result, err := docsModule.Fetch(ctx, docs.FetchOptions{
			URL: url, StartIndex: startIndex, MaxLength: maxLength,
		})
		if err != nil {
			return mcpErrorResult(err), nil
		}
		return mcpJSONResult(result)
	})
}

func mcpBoolArgument(arguments map[string]interface{}, name string) (bool, error) {
	value, exists := arguments[name]
	if !exists || value == nil {
		return false, nil
	}
	result, ok := value.(bool)
	if !ok {
		return false, fmt.Errorf("%s must be a boolean", name)
	}
	return result, nil
}

func mcpReadOnlyToolAnnotations() *protocol.ToolAnnotations {
	readOnly, destructive, idempotent, openWorld := true, false, true, false
	return &protocol.ToolAnnotations{
		ReadOnlyHint: &readOnly, DestructiveHint: &destructive, IdempotentHint: &idempotent, OpenWorldHint: &openWorld,
	}
}

func mcpExternalReadOnlyToolAnnotations() *protocol.ToolAnnotations {
	readOnly, destructive, idempotent, openWorld := true, false, true, true
	return &protocol.ToolAnnotations{
		ReadOnlyHint: &readOnly, DestructiveHint: &destructive, IdempotentHint: &idempotent, OpenWorldHint: &openWorld,
	}
}

func mcpStringArgument(arguments map[string]interface{}, name string, required bool) (string, error) {
	value, exists := arguments[name]
	if !exists || value == nil {
		if required {
			return "", fmt.Errorf("%s is required", name)
		}
		return "", nil
	}
	result, ok := value.(string)
	if !ok {
		return "", fmt.Errorf("%s must be a string", name)
	}
	if required && result == "" {
		return "", fmt.Errorf("%s is required", name)
	}
	return result, nil
}

func mcpObjectArgument(arguments map[string]interface{}, name string) (map[string]interface{}, error) {
	value, exists := arguments[name]
	if !exists || value == nil {
		return map[string]interface{}{}, nil
	}
	result, ok := value.(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("%s must be an object", name)
	}
	return result, nil
}

func mcpPositiveIntArgument(arguments map[string]interface{}, name string, defaultValue int) (int, error) {
	result, err := mcpIntArgument(arguments, name, defaultValue)
	if err != nil {
		return 0, err
	}
	if result <= 0 {
		return 0, fmt.Errorf("%s must be a positive integer", name)
	}
	return result, nil
}

func mcpNonNegativeIntArgument(arguments map[string]interface{}, name string, defaultValue int) (int, error) {
	result, err := mcpIntArgument(arguments, name, defaultValue)
	if err != nil {
		return 0, err
	}
	if result < 0 {
		return 0, fmt.Errorf("%s must be a non-negative integer", name)
	}
	return result, nil
}

func mcpIntArgument(arguments map[string]interface{}, name string, defaultValue int) (int, error) {
	value, exists := arguments[name]
	if !exists || value == nil {
		return defaultValue, nil
	}
	var result int
	switch number := value.(type) {
	case int:
		result = number
	case float64:
		if math.IsNaN(number) || math.IsInf(number, 0) || math.Trunc(number) != number {
			return 0, fmt.Errorf("%s must be an integer", name)
		}
		result = int(number)
		if float64(result) != number {
			return 0, fmt.Errorf("%s must be an integer", name)
		}
	default:
		return 0, fmt.Errorf("%s must be an integer", name)
	}
	return result, nil
}

func mcpStringArrayArgument(arguments map[string]interface{}, name string) ([]string, error) {
	value, exists := arguments[name]
	if !exists || value == nil {
		return nil, nil
	}
	switch values := value.(type) {
	case []string:
		return values, nil
	case []interface{}:
		result := make([]string, 0, len(values))
		for _, value := range values {
			text, ok := value.(string)
			if !ok {
				return nil, fmt.Errorf("%s must be an array of strings", name)
			}
			result = append(result, text)
		}
		return result, nil
	default:
		return nil, fmt.Errorf("%s must be an array of strings", name)
	}
}

func mcpJSONResult(value interface{}) (*protocol.CallToolResult, error) {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return nil, err
	}
	return &protocol.CallToolResult{Content: []protocol.Content{&protocol.TextContent{Type: "text", Text: string(data)}}}, nil
}

// mcpCompactJSONResult encodes without indentation to save tokens.
func mcpCompactJSONResult(value interface{}) (*protocol.CallToolResult, error) {
	data, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	return &protocol.CallToolResult{Content: []protocol.Content{&protocol.TextContent{Type: "text", Text: string(data)}}}, nil
}

func mcpErrorResult(err error) *protocol.CallToolResult {
	return &protocol.CallToolResult{IsError: true, Content: []protocol.Content{&protocol.TextContent{Type: "text", Text: err.Error()}}}
}
