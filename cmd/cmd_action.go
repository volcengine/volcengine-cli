package cmd

import (
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/volcengine/volcengine-cli/util"
)

type paramValue struct {
	param string
	value string
}

func generateActionCmd(serviceName string, actionMeta map[string]*VolcengineMeta, apiMetas map[string]*ApiMeta) (actionCmds []*cobra.Command) {
	for action, meta := range actionMeta {
		action := action
		var apiMeta *ApiMeta
		if len(apiMetas) > 0 {
			apiMeta = apiMetas[action]
		}
		params := exposedActionParams(meta, apiMeta)
		publicParameters := exposedActionParameterNames(meta, apiMeta)
		actionCmd := &cobra.Command{
			Use:                action,
			Short:              formatActionShort(serviceName, action),
			Long:               formatActionLong(serviceName, action),
			DisableFlagParsing: true,
			RunE: func(cmd *cobra.Command, args []string) error {
				if wantHelp, detail := parseActionHelpArgs(args); wantHelp {
					setActionHelpDetail(cmd, detail)
					// Clear after help so a later in-process Usage() does not stick on detail mode.
					defer setActionHelpDetail(cmd, false)
					return cmd.Usage()
				}
				// Bare --detail (no value) is almost always a help mistake, not a valid API call.
				// Prefer a clear hint over the generic "--detail must set value." parser error.
				if bareDetailWithoutValue(args) {
					return errBareDetailWithoutHelp()
				}

				if err := parseInvocationFlags(args, publicParameters); err != nil {
					return err
				}
				return dispatchServiceAction(ctx, cmd.Parent().Name(), cmd.Name(), true)
			},
		}

		// only used to enable auto-completion
		// todo not support application/json
		if meta.ApiInfo == nil || !isJSONContentType(meta.ApiInfo.ContentType) {
			paramValues := make([]paramValue, len(params))
			for i := 0; i < len(params); i++ {
				paramValues[i].param = params[i].key
				actionCmd.Flags().StringVar(&paramValues[i].value, paramValues[i].param, "", "")
			}

			setLazyActionUsage(actionCmd, func(detail bool) []string {
				return buildActionHelpParamLines(serviceName, action, params, nil, detail)
			})
		} else {
			var paramBody string
			actionCmd.Flags().StringVar(&paramBody, "body", "", "")
			var bodyStr []byte
			var reqParams []param
			if apiMeta != nil && apiMeta.Request != nil {
				bodyMap := apiMeta.Request.GetReqBody()
				bodyStr, _ = json.MarshalIndent(bodyMap, "", "    ")
				reqParams = apiMeta.GetRequestParams()
			}
			// Master dual-form layout (Parameter Form + JSON Form) with branch lazy/--detail.
			bodyParam := fmt.Sprintf(`body '%s'`, string(bodyStr))
			setLazyJSONActionUsage(actionCmd, bodyParam, func(detail bool) []string {
				return buildActionHelpParamLines(serviceName, action, reqParams, nil, detail)
			})
		}
		registerActionSystemFlags(actionCmd, publicParameters)

		actionCmd.Flags().BoolP("help", "h", false, "")

		actionCmds = append(actionCmds, actionCmd)
	}

	return
}

func exposedActionParams(meta *VolcengineMeta, apiMeta *ApiMeta) []param {
	if meta == nil {
		return nil
	}
	if meta.ApiInfo != nil && isJSONContentType(meta.ApiInfo.ContentType) {
		if apiMeta == nil {
			return nil
		}
		return apiMeta.GetRequestParams()
	}
	return meta.GetRequestParams(apiMeta)
}

func exposedActionParameterNames(meta *VolcengineMeta, apiMeta *ApiMeta) map[string]struct{} {
	names := make(map[string]struct{})
	for _, p := range exposedActionParams(meta, apiMeta) {
		names[p.key] = struct{}{}
	}
	if meta != nil && meta.ApiInfo != nil && isJSONContentType(meta.ApiInfo.ContentType) {
		names["body"] = struct{}{}
	}
	return names
}

func publicActionParameterNames(serviceName, action string) map[string]struct{} {
	serviceName = strings.ReplaceAll(serviceName, "_", "")
	actions := rootSupport.SupportAction[serviceName]
	if actions == nil || actions[action] == nil {
		return nil
	}
	return exposedActionParameterNames(actions[action], rootSupport.GetApiMeta(serviceName, action))
}

func registerActionSystemFlags(actionCmd *cobra.Command, actionParameters map[string]struct{}) {
	// Completion only; DisableFlagParsing means values are handled by Parser.
	// Skip names already registered or exact-name API parameters so business
	// completions win over system flags when they conflict.
	for _, name := range publicSystemFlagNames() {
		if actionCmd.Flags().Lookup(name) != nil {
			continue
		}
		if _, conflict := actionParameters[name]; conflict {
			continue
		}
		if isPresenceOnlyFixedFlag(name) {
			actionCmd.Flags().Bool(name, false, "")
			continue
		}
		actionCmd.Flags().String(name, "", "")
	}
}

func doAction(ctx *Context, serviceName, action string) error {
	if !rootSupport.IsValidAction(serviceName, action) {
		return fmt.Errorf("%s.%s is unsupport action", serviceName, action)
	}

	apiMeta := rootSupport.GetApiMeta(serviceName, action)
	method, contentType, headers, err := resolveCallStyle(ctx, serviceName, action)
	if err != nil {
		return err
	}
	version := apiVersionForCall(ctx, serviceName)
	jsonBody := isJSONContentType(contentType)

	return executeInvocation(ctx, invocationParams{
		serviceName: serviceName,
		action:      action,
		version:     version,
		method:      method,
		contentType: contentType,
		headers:     headers,
	}, func() (invocationInput, error) {
		input, fromBody, err := buildActionInput(ctx.dynamicFlags.flags, apiMeta, jsonBody)
		if err != nil {
			return invocationInput{}, err
		}
		return invocationInput{value: input, jsonBody: jsonBody, fromBody: fromBody}, nil
	})
}

type invocationParams struct {
	serviceName string
	action      string
	version     string
	method      string
	contentType string
	headers     []requestHeader
}

type invocationInput struct {
	value    interface{}
	jsonBody bool
	fromBody bool
}

// sdkCallInput normalizes invocationInput to the shape CallSdk expects:
//   - fromBody: already pointer-shaped (*map or *[] from parseJSONBody)
//   - otherwise: map value from expandFlatToJSON / flat non-JSON path → *map
func sdkCallInput(built invocationInput) (interface{}, error) {
	if built.fromBody {
		return built.value, nil
	}
	m, ok := built.value.(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("internal error: action input must be a map, got %T", built.value)
	}
	return &m, nil
}

// executeInvocationHook 仅供测试注入，避免单测触发真实 SDK 调用。
var executeInvocationHook func(ctx *Context, p invocationParams, buildInput func() (invocationInput, error)) error

func executeInvocation(ctx *Context, p invocationParams, buildInput func() (invocationInput, error)) (err error) {
	if executeInvocationHook != nil {
		return executeInvocationHook(ctx, p, buildInput)
	}
	debugLog, closeDebugLog, err := prepareDebugLogger(ctx)
	if err != nil {
		return err
	}
	defer func() {
		if closeErr := closeDebugLog(); err == nil && closeErr != nil {
			err = closeErr
		}
	}()

	debugLogActionStart(debugLog, p.serviceName, p.action, p.version, p.method, p.contentType)

	sdk, err := NewSimpleClient(ctx)
	if err != nil {
		debugLogError(debugLog, "client_init_error", err)
		return err
	}

	built, err := buildInput()
	if err != nil {
		debugLogError(debugLog, "input_build_error", err)
		return err
	}
	debugLogInput(debugLog, ctx.dynamicFlags.flags, built.value, built.fromBody)

	sdkServiceName := p.serviceName
	if svc, ok := GetServiceMapping(p.serviceName); ok {
		sdkServiceName = svc
	}

	info := SdkClientInfo{
		ServiceName: sdkServiceName,
		Action:      p.action,
		Version:     p.version,
		Method:      p.method,
		ContentType: p.contentType,
		Headers:     p.headers,
	}

	input, err := sdkCallInput(built)
	if err != nil {
		debugLogError(debugLog, "input_type_error", err)
		return err
	}

	start := time.Now()
	out, err := sdk.CallSdk(info, input)
	if err != nil {
		debugLogSdkEnd(debugLog, start, err)
		return formatActionError(err)
	}
	debugLogSdkEnd(debugLog, start, nil)

	if config == nil || !config.EnableColor {
		util.ShowJson(*out, false)
	} else {
		util.ShowJson(*out, true)
	}
	return nil
}

// resolveActionHTTPMethod 决定 HTTP 方法（正常路径与 force 共用）：元数据优先，显式 ---method 可覆盖。
func resolveActionHTTPMethod(ctx *Context, apiInfo *ApiInfo) (string, error) {
	method := "GET"
	if apiInfo != nil && apiInfo.Method != "" {
		method = apiInfo.Method
	}
	override, err := explicitHTTPMethod(ctx)
	if err != nil {
		return "", err
	}
	if override != "" {
		method = override
	}
	return method, nil
}

func prepareDebugLogger(ctx *Context) (*DebugLogger, func() error, error) {
	if ctx != nil && ctx.debugLogger != nil {
		return ctx.debugLogger, func() error { return nil }, nil
	}

	opts, err := resolveDebugOptions()
	if err != nil {
		return nil, nil, err
	}
	logger, err := newDebugLogger(opts)
	if err != nil {
		return nil, nil, err
	}
	if ctx != nil {
		ctx.debugLogger = logger
	}
	return logger, func() error {
		closeErr := logger.Close()
		if ctx != nil && ctx.debugLogger == logger {
			ctx.debugLogger = nil
		}
		return closeErr
	}, nil
}

func debugLogActionStart(logger *DebugLogger, serviceName, action, version, method, contentType string) {
	if !logger.Enabled() {
		return
	}
	logger.Printf("action_start service=%s action=%s version=%s method=%s content_type=%s",
		serviceName, action, version, method, contentType)
}

func debugLogInput(logger *DebugLogger, flags []*Flag, input interface{}, inputFromBody bool) {
	if !logger.Enabled() {
		return
	}
	names := make([]string, 0, len(flags))
	for _, f := range flags {
		if f != nil {
			names = append(names, f.Name)
		}
	}
	sort.Strings(names)
	logger.Printf("action_input input_from_body=%t dynamic_params=%s input=%s",
		inputFromBody, strings.Join(names, ","), formatDebugValue(input, defaultDebugValueLimit))
}

func debugLogSdkEnd(logger *DebugLogger, start time.Time, callErr error) {
	if !logger.Enabled() {
		return
	}
	duration := time.Since(start)
	if callErr != nil {
		logger.Printf("sdk_call_error duration_ms=%d error=%s", duration/time.Millisecond, callErr.Error())
		return
	}
	logger.Printf("sdk_call_success duration_ms=%d", duration/time.Millisecond)
}

func debugLogError(logger *DebugLogger, stage string, stageErr error) {
	if !logger.Enabled() || stageErr == nil {
		return
	}
	logger.Printf("%s error=%s", stage, stageErr.Error())
}

func formatActionError(err error) error {
	if err == nil {
		return nil
	}
	if strings.Contains(err.Error(), "NoCredentialProviders") || strings.Contains(err.Error(), "no valid providers in chain") {
		return fmt.Errorf("credentials not configured, please run 've login' or 've configure set', or set VOLCENGINE_ACCESS_KEY and VOLCENGINE_SECRET_KEY environment variables")
	}
	return err
}

// isStringParam reports whether the named parameter should be treated as a
// literal string when rebuilding request input.
//
// This includes both parameters declared as type "string" and indexed
// elements of repeated string arrays whose metadata key ends with ".N" and is
// declared as "array[string]" (for example ResourceNames.N -> --ResourceNames.0).
// In those cases the caller must NOT attempt to parse the value as JSON.
func isStringParam(apiMeta *ApiMeta, name string) bool {
	mt, matchedKey, ok := getRequestMetaType(apiMeta, name)
	if !ok {
		return false
	}

	switch mt.TypeName {
	case "string":
		return true
	case "array[string]":
		return isIndexedStringArrayElement(matchedKey)
	default:
		return false
	}
}

func isIndexedStringArrayElement(matchedKey string) bool {
	return strings.HasSuffix(matchedKey, ".N")
}

func getRequestMetaType(apiMeta *ApiMeta, name string) (*MetaType, string, bool) {
	if apiMeta == nil || apiMeta.Request == nil || apiMeta.Request.MetaTypes == nil {
		return nil, "", false
	}

	if mt, ok := apiMeta.Request.MetaTypes[name]; ok {
		return mt, name, true
	}

	normalizedName := normalizeMetaTypeKey(name)
	if normalizedName == name {
		return nil, "", false
	}

	mt, ok := apiMeta.Request.MetaTypes[normalizedName]
	return mt, normalizedName, ok
}

func normalizeMetaTypeKey(name string) string {
	parts := strings.Split(name, ".")
	changed := false

	for i, part := range parts {
		if _, err := strconv.Atoi(part); err == nil {
			parts[i] = "N"
			changed = true
		}
	}

	if !changed {
		return name
	}

	return strings.Join(parts, ".")
}
