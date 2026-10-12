package cmd

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/ThinkInAIXYZ/go-mcp/protocol"
	"github.com/ThinkInAIXYZ/go-mcp/server"
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

func registerMCPTools(registrar mcpToolRegistrar, support *RootSupport, invoker mcpAPIInvoker) {
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
