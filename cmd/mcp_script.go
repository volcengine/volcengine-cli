package cmd

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/ThinkInAIXYZ/go-mcp/protocol"
	"github.com/volcengine/volcengine-cli/internal/safety"
	"github.com/volcengine/volcengine-cli/internal/scriptrun"
)

const mcpRunScriptDescription = `Run a Python script that orchestrates several Volcano Engine API calls with loops, branches, pagination, data processing and bounded asyncio concurrency. Use call_api for a single API call.
Contract: CPython 3.9+ standard library only (no third-party packages). Top-level await is allowed.
- await call_api(service, api, parameters=None, region=None) calls one API through this CLI and returns the full response dict; the API data is under response["Result"] (for example response["Result"]["Vpcs"]). Failures raise APIError with .code, .status (rejected = not sent, failed = API error, unknown = may have reached the API), .request_id and .seq.
- The global args holds the "args" input. Assign a JSON-serializable value to the global result. print() output is captured.
- Every call is checked against the local safety policy. A deny rule rejects that call with an APIError. A script is approved as a whole: without yes, the first call a confirm rule protects is not sent and stops the run; ask the user to approve the script, then run it again unchanged with yes=true, which lets every confirm call of that run through. Never perform the protected operations one by one with call_api instead.
- Calls are bounded by max_calls (the first extra call is rejected; further calls stop the run), max_concurrency, call_timeout_seconds and timeout_seconds. The result includes a journal of every call, including partial successes and unknown outcomes after cancellation or timeout. Apart from the one approved rerun after SafetyConfirmationRequired, never rerun a partially executed script; continue from the journal.
Trust: the script runs as trusted local code with the user's OS privileges. It is not a sandbox.`

func mcpRunScriptInputSchema() json.RawMessage {
	defaults, maximum := scriptrun.DefaultLimits(), scriptrun.MaxLimits()
	schema := map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"code": map[string]interface{}{"type": "string", "minLength": 1, "maxLength": defaults.MaxCodeBytes, "description": "Python source. Use await call_api(...) and assign result."},
			"args": map[string]interface{}{"description": "Optional JSON value exposed to the script as args."},
			"max_calls": map[string]interface{}{"type": "integer", "minimum": 1, "maximum": maximum.MaxCalls, "default": defaults.MaxCalls,
				"description": "Maximum API calls for the run."},
			"max_concurrency": map[string]interface{}{"type": "integer", "minimum": 1, "maximum": maximum.MaxConcurrency, "default": defaults.MaxConcurrency,
				"description": "Maximum API calls in flight at once; extra calls wait."},
			"timeout_seconds": map[string]interface{}{"type": "integer", "minimum": 1, "maximum": int(maximum.Timeout.Seconds()), "default": int(defaults.Timeout.Seconds()),
				"description": "Wall-clock limit for the whole run."},
			"call_timeout_seconds": map[string]interface{}{"type": "integer", "minimum": 1, "maximum": int(maximum.CallTimeout.Seconds()), "default": int(defaults.CallTimeout.Seconds()),
				"description": "Limit for each API call; a timed-out call has an unknown outcome."},
			"yes": map[string]interface{}{"type": "boolean", "default": false,
				"description": "Approves the whole script for this run. " + safety.MCPApprovalDescription},
		},
		"required":             []string{"code"},
		"additionalProperties": false,
	}
	// The schema contains only maps, slices, strings and numbers.
	data, _ := json.Marshal(schema)
	return data
}

func registerMCPRunScriptTool(registrar mcpToolRegistrar, runner mcpScriptRunner) {
	tool := protocol.NewToolWithRawSchema("run_script", mcpRunScriptDescription, mcpRunScriptInputSchema())
	readOnly, destructive, idempotent, openWorld := false, true, false, true
	tool.Annotations = &protocol.ToolAnnotations{
		ReadOnlyHint: &readOnly, DestructiveHint: &destructive, IdempotentHint: &idempotent, OpenWorldHint: &openWorld,
	}
	registrar.RegisterTool(tool, func(ctx context.Context, request *protocol.CallToolRequest) (*protocol.CallToolResult, error) {
		code, err := mcpStringArgument(request.Arguments, "code", true)
		if err != nil {
			return mcpErrorResult(err), nil
		}
		args, err := scriptArgsArgument(request.Arguments)
		if err != nil {
			return mcpErrorResult(err), nil
		}
		limits, err := scriptLimitsFromArguments(request.Arguments)
		if err != nil {
			return mcpErrorResult(err), nil
		}
		yes, err := mcpBoolArgument(request.Arguments, "yes")
		if err != nil {
			return mcpErrorResult(err), nil
		}
		report, err := runner.RunScript(ctx, scriptrun.Request{Code: code, Args: args, Limits: limits, Approved: yes})
		if err != nil {
			return mcpErrorResult(err), nil
		}
		result, err := mcpJSONResult(report)
		if err != nil {
			return mcpErrorResult(fmt.Errorf("encode run_script result: %w", err)), nil
		}
		// Anything but success is a tool error so agents inspect the journal;
		// the full report is still returned.
		result.IsError = report.Status != scriptrun.RunStatusSucceeded
		return result, nil
	})
}
