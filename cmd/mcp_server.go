package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"math"

	"github.com/ThinkInAIXYZ/go-mcp/protocol"
	"github.com/ThinkInAIXYZ/go-mcp/server"
	"github.com/volcengine/volcengine-cli/internal/docs"
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
	CallAPI(context.Context, string, string, string, map[string]interface{}) (map[string]interface{}, error)
}

type mcpDocsModule interface {
	Search(context.Context, docs.SearchOptions) (docs.SearchResult, error)
	Fetch(context.Context, docs.FetchOptions) (docs.FetchResult, error)
}

func registerMCPTools(registrar mcpToolRegistrar, support *RootSupport, invoker mcpAPIInvoker, docsModule mcpDocsModule) {
	listServicesTool := protocol.NewToolWithRawSchema("list_services", "List every Volcano Engine service code supported by this CLI. Use the returned service code with list_apis; no search query is needed.", json.RawMessage(`{"type":"object","properties":{}}`))
	listServicesTool.Annotations = mcpReadOnlyToolAnnotations()
	registrar.RegisterTool(listServicesTool, func(_ context.Context, _ *protocol.CallToolRequest) (*protocol.CallToolResult, error) {
		return mcpJSONResult(listMCPServices(support))
	})

	listAPIsTool := protocol.NewToolWithRawSchema("list_apis", "List every supported API action for one Volcano Engine service code returned by list_services.", json.RawMessage(`{"type":"object","properties":{"service":{"type":"string"}},"required":["service"]}`))
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

	callAPITool := protocol.NewToolWithRawSchema("call_api", "Call any supported Volcano Engine API. Cloud IAM permissions determine whether the call is allowed.", json.RawMessage(`{"type":"object","properties":{"service":{"type":"string"},"api":{"type":"string"},"region":{"type":"string","description":"Override the region for this API call.","pattern":"^[A-Za-z0-9][A-Za-z0-9-]{0,127}$"},"parameters":{"type":"object","additionalProperties":true}},"required":["service","api"]}`))
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
		result, err := invoker.CallAPI(ctx, serviceName, api, region, parameters)
		if err != nil {
			return mcpErrorResult(err), nil
		}
		return mcpJSONResult(result)
	})

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

func mcpErrorResult(err error) *protocol.CallToolResult {
	return &protocol.CallToolResult{IsError: true, Content: []protocol.Content{&protocol.TextContent{Type: "text", Text: err.Error()}}}
}
