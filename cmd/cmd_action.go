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
				if len(args) == 1 && (args[0] == "-h" || args[0] == "--help") {
					cmd.Usage()
					return nil
				}

				parser := NewParser(args, publicParameters)
				if _, err := parser.ReadArgs(ctx); err != nil {
					return err
				}

				return doAction(ctx, cmd.Parent().Name(), cmd.Name())
			},
		}

		// only used to enable auto-completion
		// todo not support application/json
		if meta.ApiInfo == nil || strings.ToLower(meta.ApiInfo.ContentType) != "application/json" {
			paramValues := make([]paramValue, len(params))
			for i := 0; i < len(params); i++ {
				paramValues[i].param = params[i].key
				actionCmd.Flags().StringVar(&paramValues[i].value, paramValues[i].param, "", "")
			}

			actionCmd.SetUsageTemplate(actionUsageTemplate(actionCmd.Long, formatParamsHelpUsage(params)))
		} else {
			var paramBody string
			actionCmd.Flags().StringVar(&paramBody, "body", "", "")
			var bodyStr []byte
			var paramHelp []string
			if apiMeta != nil && apiMeta.Request != nil {
				bodyMap := apiMeta.Request.GetReqBody()
				bodyStr, _ = json.MarshalIndent(bodyMap, "", "    ")
				paramHelp = formatParamsHelpUsage(params)
			}
			bodyParam := fmt.Sprintf(`body '%s'`, string(bodyStr))
			actionCmd.SetUsageTemplate(jsonActionUsageTemplate(actionCmd.Long, paramHelp, bodyParam))
		}
		registerActionSystemFlags(actionCmd)

		actionCmd.Flags().BoolP("help", "h", false, "")

		actionCmds = append(actionCmds, actionCmd)
	}

	return
}

func exposedActionParams(meta *VolcengineMeta, apiMeta *ApiMeta) []param {
	if meta == nil {
		return nil
	}
	if meta.ApiInfo != nil && strings.EqualFold(meta.ApiInfo.ContentType, "application/json") {
		return apiMeta.GetRequestParams()
	}
	return meta.GetRequestParams(apiMeta)
}

func exposedActionParameterNames(meta *VolcengineMeta, apiMeta *ApiMeta) map[string]struct{} {
	names := make(map[string]struct{})
	for _, p := range exposedActionParams(meta, apiMeta) {
		names[p.key] = struct{}{}
	}
	if meta != nil && meta.ApiInfo != nil && strings.EqualFold(meta.ApiInfo.ContentType, "application/json") {
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

func registerActionSystemFlags(actionCmd *cobra.Command) {
	for _, name := range []string{"profile", "region", "endpoint", "lang"} {
		if actionCmd.Flags().Lookup(name) != nil {
			continue
		}
		actionCmd.Flags().String(name, "", "")
	}
}

func doAction(ctx *Context, serviceName, action string) (err error) {
	if !rootSupport.IsValidAction(serviceName, action) {
		err = fmt.Errorf("%s.%s is unsupport action", serviceName, action)
		return
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

	var (
		sdk *SdkClient
		out *map[string]interface{}
	)

	method := "GET"
	contentType := ""
	apiInfo := rootSupport.GetApiInfo(serviceName, action)
	apiMeta := rootSupport.GetApiMeta(serviceName, action)

	if apiInfo != nil && apiInfo.Method != "" {
		method = apiInfo.Method
	}

	if apiInfo != nil && apiInfo.ContentType != "" {
		contentType = apiInfo.ContentType
	}

	version := rootSupport.GetVersion(serviceName)
	debugLogActionStart(debugLog, serviceName, action, version, method, contentType)

	sdk, err = NewSimpleClient(ctx)
	if err != nil {
		debugLogError(debugLog, "client_init_error", err)
		return
	}

	jsonBody := strings.ToLower(contentType) == "application/json"
	input, inputFromBody, err := buildActionInput(ctx.dynamicFlags.flags, apiMeta, jsonBody)
	if err != nil {
		debugLogError(debugLog, "input_build_error", err)
		return
	}
	debugLogInput(debugLog, ctx.dynamicFlags.flags, input, inputFromBody)

	if svc, ok := GetServiceMapping(serviceName); ok {
		serviceName = svc
	}

	start := time.Now()
	if strings.ToLower(contentType) != "application/json" {
		inputMap, _ := input.(map[string]interface{})
		out, err = sdk.CallSdk(SdkClientInfo{
			ServiceName: serviceName,
			Action:      action,
			Version:     version,
			Method:      method,
			ContentType: contentType,
		}, &inputMap)
	} else {
		if !inputFromBody {
			inputMap, _ := input.(map[string]interface{})
			input = &inputMap
		}
		out, err = sdk.CallSdk(SdkClientInfo{
			ServiceName: serviceName,
			Action:      action,
			Version:     version,
			Method:      method,
			ContentType: contentType,
		}, input)
	}
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
	return
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

func actionUsageTemplate(description string, params []string) string {
	return renderActionUsageTemplate(description, formatActionUsageParams(params, "  "))
}

func jsonActionUsageTemplate(description string, params []string, bodyParam string) string {
	sections := make([]string, 0, 2)
	if len(params) > 0 {
		sections = append(sections, fmt.Sprintf("  %s\n%s", tr("Parameter Form:"), formatActionUsageParams(params, "    ")))
	}
	if bodyParam != "" {
		sections = append(sections, fmt.Sprintf("  %s\n%s", tr("JSON Form:"), formatActionUsageParams([]string{bodyParam}, "    ")))
	}

	parameterHelp := strings.Join(sections, "\n\n")
	if parameterHelp != "" {
		parameterHelp = "\n" + parameterHelp
	}
	return renderActionUsageTemplate(description, parameterHelp)
}

func formatActionUsageParams(params []string, indent string) string {
	formatted := append([]string(nil), params...)
	sort.Strings(formatted)

	for i := 0; i < len(formatted); i++ {
		param := "--" + formatted[i]
		formatted[i] = indent + strings.ReplaceAll(param, "\n", "\n"+indent)
	}
	return strings.Join(formatted, "\n")
}

func renderActionUsageTemplate(description, parameterHelp string) string {
	description = strings.TrimSpace(description)
	if description != "" {
		description += "\n\n"
	}

	return fmt.Sprintf(`%s%s{{if .Runnable}}
  {{.CommandPath}} [params]{{end}}{{if .HasExample}}

%s
{{.Example}}{{end}}

%s
%s

%s
%s

`, description, tr("Usage:"), tr("Examples:"), tr("API Parameters:"), parameterHelp,
		tr("System Flags (place before service):"), localizedSystemFlagsHelp())
}
