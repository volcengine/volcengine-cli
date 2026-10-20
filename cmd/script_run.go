package cmd

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/volcengine/volcengine-cli/internal/pyruntime"
	"github.com/volcengine/volcengine-cli/internal/safety"
	"github.com/volcengine/volcengine-cli/internal/scriptrun"
	"github.com/volcengine/volcengine-go-sdk/volcengine/volcengineerr"
)

const (
	// scriptPythonEnvironment selects an explicit, validated interpreter.
	scriptPythonEnvironment = "VOLCENGINE_SCRIPT_PYTHON"

	scriptErrorUnsupportedAPI       = "UnsupportedAPI"
	scriptErrorSafetyDenied         = "SafetyDenied"
	scriptErrorSafetyConfirmation   = "SafetyConfirmationRequired"
	scriptErrorSafetyPolicy         = "SafetyPolicyError"
	scriptErrorCredentials          = "CredentialError"
	scriptErrorIdentityChanged      = "IdentityChanged"
	scriptErrorRequest              = "RequestError"
	scriptTrustedExecutionStatement = "run_script executed trusted local Python code with the current OS user's privileges; it is not a sandbox."
)

// scriptRunBinding is the identity a run is bound to. Every API request of
// the run must resolve to the same profile, credential source and identity;
// credentials may refresh for that identity.
type scriptRunBinding struct {
	Profile          string `json:"profile,omitempty"`
	CredentialSource string `json:"credential_source"`
	Region           string `json:"region"`
	identity         string
}

// scriptAPIInvoker executes script API requests through the same client
// resolution and SDK call path as call_api.
type scriptAPIInvoker struct {
	base     *volcengineMCPInvoker
	metadata mcpRequestMetadata
	binding  scriptRunBinding
}

func newScriptAPIInvoker(base *volcengineMCPInvoker, metadata mcpRequestMetadata) (*scriptAPIInvoker, error) {
	if base == nil {
		return nil, fmt.Errorf("API invoker is not configured")
	}
	client, err := base.resolveClient("", metadata)
	if err != nil {
		return nil, err
	}
	return &scriptAPIInvoker{
		base: base, metadata: metadata,
		binding: scriptRunBinding{
			Profile: client.profileName, CredentialSource: client.credentialSource,
			Region: client.region, identity: client.identity,
		},
	}, nil
}

// Invoke implements scriptrun.Invoker. Validation, safety policy, identity
// binding and credentials are evaluated for every request.
func (s *scriptAPIInvoker) Invoke(ctx context.Context, call scriptrun.Call) (map[string]interface{}, *scriptrun.CallError) {
	rejected := func(code, message string) *scriptrun.CallError {
		return &scriptrun.CallError{Code: code, Status: scriptrun.CallStatusRejected, Message: message}
	}
	region := s.binding.Region
	if call.Region != "" {
		normalized, err := normalizeMCPRegion(call.Region)
		if err != nil {
			return nil, rejected(scriptrun.ErrorCodeInvalidRequest, err.Error())
		}
		region = normalized
	}
	if !rootSupport.IsValidAction(call.Service, call.API) {
		return nil, rejected(scriptErrorUnsupportedAPI, fmt.Sprintf(tr("unsupported API %s.%s; use search_api to find supported service codes and actions"), call.Service, call.API))
	}
	if callErr := checkScriptSafetyPolicy(call.Service, call.API); callErr != nil {
		return nil, callErr
	}
	client, err := s.base.resolveClient(region, s.metadata)
	if err != nil {
		return nil, rejected(scriptErrorCredentials, err.Error())
	}
	if client.profileName != s.binding.Profile || client.credentialSource != s.binding.CredentialSource || client.identity != s.binding.identity {
		callErr := rejected(scriptErrorIdentityChanged, tr("the credential identity changed during the run (profile, credential source or identity); the run was stopped and no further API calls were sent"))
		callErr.Fatal = true
		return nil, callErr
	}
	output, err := callAPIWithClientContext(ctx, client.sdk, call.Service, call.API, call.Parameters, false)
	if err != nil {
		var failure volcengineerr.RequestFailure
		if errors.As(err, &failure) {
			return nil, &scriptrun.CallError{Code: failure.Code(), Status: scriptrun.CallStatusFailed, Message: failure.Message(), RequestID: failure.RequestID()}
		}
		// Without an API response the request may or may not have reached
		// the service, so the outcome is unknown.
		return nil, &scriptrun.CallError{Code: scriptErrorRequest, Status: scriptrun.CallStatusUnknown, Message: formatActionError(err).Error()}
	}
	if output == nil {
		return map[string]interface{}{}, nil
	}
	return *output, nil
}

// checkScriptSafetyPolicy applies the safety policy to one script request.
// Unlike call_api, a script has no approval input: confirm rules always
// reject the request and VOLCENGINE_SAFETY_SKIP_CONFIRM is deliberately not
// honored, so protected operations need a separate, approved call_api.
func checkScriptSafetyPolicy(service, action string) *scriptrun.CallError {
	_, policy, err := loadEffectiveSafetyPolicy()
	if err != nil {
		return &scriptrun.CallError{Code: scriptErrorSafetyPolicy, Status: scriptrun.CallStatusRejected, Message: err.Error()}
	}
	result := policy.Check(safety.CommandInfo{Product: service, ProductAliases: safetyServiceAliases(service), Action: action})
	switch result.Action {
	case safety.ActionDeny:
		return &scriptrun.CallError{Code: scriptErrorSafetyDenied, Status: scriptrun.CallStatusRejected,
			Message: fmt.Sprintf(tr("operation blocked by safety policy: %s %s (rule: %s)"), service, action, result.Rule.Pattern)}
	case safety.ActionConfirm:
		return &scriptrun.CallError{Code: scriptErrorSafetyConfirmation, Status: scriptrun.CallStatusRejected,
			Message: fmt.Sprintf(tr("safety policy requires confirmation for: %s %s. run_script cannot approve protected operations and was not sent. Do not rerun the script: review its call journal, explain the target and impact, wait for a NEW user reply approving this one operation, then perform only it with call_api and yes=true once."), service, action)}
	}
	return nil
}

// scriptRunReport is the run_script result returned to MCP clients and the
// CLI.
type scriptRunReport struct {
	*scriptrun.Result
	Binding scriptRunBinding       `json:"binding"`
	Runtime *pyruntime.Interpreter `json:"runtime"`
	Trust   string                 `json:"trust"`
}

// mcpScriptRunner runs one script request. The local runner executes trusted
// CPython on this host; a hosted sandbox adapter can implement the same
// interface with another scriptrun.Executor.
type mcpScriptRunner interface {
	RunScript(ctx context.Context, request scriptrun.Request) (*scriptRunReport, error)
}

type localScriptRunner struct {
	invoker *volcengineMCPInvoker
	// interpreter is selected once, when the MCP server starts or before a
	// CLI run, and reused for every run without repeating discovery.
	interpreter *pyruntime.Interpreter
}

// RunScript binds the run to the current identity and executes the script
// with the selected interpreter. It never resolves or downloads Python.
func (r *localScriptRunner) RunScript(ctx context.Context, request scriptrun.Request) (*scriptRunReport, error) {
	if r.interpreter == nil {
		return nil, errors.New(tr("no Python interpreter was selected for run_script"))
	}
	if info, err := os.Stat(r.interpreter.Path); err != nil || info.IsDir() {
		return nil, fmt.Errorf(tr("the selected Python interpreter %s is no longer available; restart the MCP server or rerun the command to select an interpreter again"), r.interpreter.Path)
	}
	invoker, err := newScriptAPIInvoker(r.invoker, mcpRequestMetadataFromContext(ctx))
	if err != nil {
		return nil, err
	}
	executor := &scriptrun.LocalExecutor{Python: r.interpreter.Path}
	result, err := executor.Execute(ctx, request, invoker)
	if err != nil {
		return nil, err
	}
	return &scriptRunReport{Result: result, Binding: invoker.binding, Runtime: r.interpreter, Trust: scriptTrustedExecutionStatement}, nil
}

// resolveScriptPython selects the interpreter: an explicit flag value, else
// VOLCENGINE_SCRIPT_PYTHON, else a usable local python3/python on PATH, else
// the ve-managed runtime. It never downloads.
func resolveScriptPython(ctx context.Context, explicit string) (*pyruntime.Interpreter, error) {
	if strings.TrimSpace(explicit) == "" {
		explicit = strings.TrimSpace(os.Getenv(scriptPythonEnvironment))
	}
	configDir, err := configFileDirFunc()
	if err != nil {
		return nil, err
	}
	interpreter, err := pyruntime.Resolve(ctx, explicit, pyruntime.Root(configDir))
	if errors.Is(err, pyruntime.ErrNotPrepared) {
		message := tr("the run_script Python runtime is not prepared: no usable CPython 3.9+ (python3 or python) was found on PATH and no ve-managed runtime is installed; install Python 3.9+, run 've script runtime install', or set VOLCENGINE_SCRIPT_PYTHON")
		if detail := strings.TrimPrefix(err.Error(), pyruntime.ErrNotPrepared.Error()+"; "); detail != err.Error() {
			message += " (" + detail + ")"
		}
		return nil, errors.New(message)
	}
	return interpreter, err
}

// scriptLimitsFromArguments applies optional per-run limit overrides.
func scriptLimitsFromArguments(arguments map[string]interface{}) (scriptrun.Limits, error) {
	limits := scriptrun.DefaultLimits()
	maximum := scriptrun.MaxLimits()
	integer := func(name string, target *int, upper int) error {
		value, err := mcpPositiveIntArgument(arguments, name, *target)
		if err != nil {
			return err
		}
		if value > upper {
			return fmt.Errorf("%s must be at most %d", name, upper)
		}
		*target = value
		return nil
	}
	if err := integer("max_calls", &limits.MaxCalls, maximum.MaxCalls); err != nil {
		return limits, err
	}
	if err := integer("max_concurrency", &limits.MaxConcurrency, maximum.MaxConcurrency); err != nil {
		return limits, err
	}
	timeout := int(limits.Timeout.Seconds())
	if err := integer("timeout_seconds", &timeout, int(maximum.Timeout.Seconds())); err != nil {
		return limits, err
	}
	callTimeout := int(limits.CallTimeout.Seconds())
	if err := integer("call_timeout_seconds", &callTimeout, int(maximum.CallTimeout.Seconds())); err != nil {
		return limits, err
	}
	limits.Timeout = time.Duration(timeout) * time.Second
	limits.CallTimeout = time.Duration(callTimeout) * time.Second
	return limits, limits.Validate()
}

func scriptArgsArgument(arguments map[string]interface{}) (json.RawMessage, error) {
	value, exists := arguments["args"]
	if !exists || value == nil {
		return json.RawMessage("null"), nil
	}
	data, err := json.Marshal(value)
	if err != nil {
		return nil, fmt.Errorf("args must be JSON: %w", err)
	}
	return data, nil
}
