package cmd

import (
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"

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

				parser := NewParser(args)
				if _, err := parser.ReadArgs(ctx); err != nil {
					return err
				}

				return doAction(ctx, cmd.Parent().Name(), cmd.Name())
			},
		}

		// only used to enable auto-completion
		// todo not support application/json
		if meta.ApiInfo == nil || strings.ToLower(meta.ApiInfo.ContentType) != "application/json" {
			params := meta.GetRequestParams(apiMeta)
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
			params := []string{fmt.Sprintf(`body '%s'`, string(bodyStr))}
			if apiMeta != nil && apiMeta.Request != nil {
				bodyMap := apiMeta.Request.GetReqBody()
				bodyStr, _ = json.MarshalIndent(bodyMap, "", "    ")
				params = append([]string{fmt.Sprintf(`body '%s'`, string(bodyStr))}, formatParamsHelpUsage(apiMeta.GetRequestParams())...)
			}
			actionCmd.SetUsageTemplate(actionUsageTemplate(actionCmd.Long, params))
		}

		actionCmd.Flags().BoolP("help", "h", false, "")

		actionCmds = append(actionCmds, actionCmd)
	}

	return
}

func doAction(ctx *Context, serviceName, action string) (err error) {
	if !rootSupport.IsValidAction(serviceName, action) {
		err = fmt.Errorf("%s.%s is unsupport action", serviceName, action)
		return
	}

	var (
		sdk *SdkClient
		out *map[string]interface{}
	)
	sdk, err = NewSimpleClient(ctx)
	if err != nil {
		return
	}

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

	jsonBody := strings.ToLower(contentType) == "application/json"
	input, inputFromBody, err := buildActionInput(ctx.dynamicFlags.flags, apiMeta, jsonBody)
	if err != nil {
		return
	}

	version := rootSupport.GetVersion(serviceName)

	if svc, ok := GetServiceMapping(serviceName); ok {
		serviceName = svc
	}

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
		return formatActionError(err)
	}

	if config == nil || !config.EnableColor {
		util.ShowJson(*out, false)
	} else {
		util.ShowJson(*out, true)
	}
	return
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
	sort.Strings(params)

	for i := 0; i < len(params); i++ {
		params[i] = "  --" + params[i]
	}

	description = strings.TrimSpace(description)
	if description != "" {
		description += "\n\n"
	}

	return fmt.Sprintf(`%sUsage:{{if .Runnable}}
  {{.CommandPath}} [params]{{end}}{{if .HasExample}}

Examples:
{{.Example}}{{end}}

Available Parameters:
%s

Fixed Flags:
  ---profile string    Use a configured profile only for this invocation.
  ---region string     Override the region only for this invocation.
  ---endpoint string   Override the endpoint only for this invocation.

`, description, strings.Join(params, "\n"))
}
