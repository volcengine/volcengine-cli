package cmd

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/spf13/cobra"
	"github.com/volcengine/volcengine-cli/util"
)

type paramValue struct {
	param string
	value string
}

func generateActionCmd(actionMeta map[string]*VolcengineMeta, apiMetas map[string]*ApiMeta) (actionCmds []*cobra.Command) {
	for action, meta := range actionMeta {
		var apiMeta *ApiMeta
		if len(apiMetas) > 0 {
			apiMeta = apiMetas[action]
		}
		actionCmd := &cobra.Command{
			Use:                action,
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
		if meta.ApiInfo == nil {
			params := meta.GetRequestParams(apiMeta)
			paramValues := make([]paramValue, len(params))
			for i := 0; i < len(params); i++ {
				paramValues[i].param = params[i]
				actionCmd.Flags().StringVar(&paramValues[i].value, paramValues[i].param, "", "")
			}
			actionCmd.SetUsageTemplate(actionUsageTemplate(params))
		} else {
			var paramBody string
			actionCmd.Flags().StringVar(&paramBody, "body", "", "")
			actionCmd.SetUsageTemplate(actionUsageTemplate([]string{"body"}))
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

	if apiInfo != nil && apiInfo.Method != "" {
		method = apiInfo.Method
	}

	if apiInfo != nil && apiInfo.ContentType != "" {
		contentType = apiInfo.ContentType
	}

	input := make(map[string]interface{})
	for _, f := range ctx.dynamicFlags.flags {
		// rebuild input
		if f.Name != "body" {
			if a, success := util.ParseToJsonArrayOrObject(strings.TrimSpace(f.value)); success {
				input[f.Name] = a
			} else {
				input[f.Name] = f.value
			}
		} else {
			// origin
			input[f.Name] = f.value
		}
	}

	if svc, ok := GetServiceMapping(serviceName); ok {
		serviceName = svc
	}

	if strings.ToLower(contentType) != "application/json" {
		out, err = sdk.CallSdk(SdkClientInfo{
			ServiceName: serviceName,
			Action:      action,
			Version:     rootSupport.GetVersion(serviceName),
			Method:      method,
			ContentType: contentType,
		}, &input)
	} else {
		if jsonStr, ok := input["body"]; ok {
			var (
				a []interface{}
			)
			m := make(map[string]interface{})
			err = json.Unmarshal([]byte(jsonStr.(string)), &m)
			if err != nil {
				err = json.Unmarshal([]byte(jsonStr.(string)), &a)
				if err != nil {
					fmt.Println("json format error")
					return
				}
				out, err = sdk.CallSdk(SdkClientInfo{
					ServiceName: serviceName,
					Action:      action,
					Version:     rootSupport.GetVersion(serviceName),
					Method:      method,
					ContentType: contentType,
				}, &a)
			} else {
				out, err = sdk.CallSdk(SdkClientInfo{
					ServiceName: serviceName,
					Action:      action,
					Version:     rootSupport.GetVersion(serviceName),
					Method:      method,
					ContentType: contentType,
				}, &m)
			}
		} else {
			out, err = sdk.CallSdk(SdkClientInfo{
				ServiceName: serviceName,
				Action:      action,
				Version:     rootSupport.GetVersion(serviceName),
				Method:      method,
				ContentType: contentType,
			}, &input)
		}
	}

	if config == nil || !config.EnableColor {
		util.ShowJson(*out, false)
	} else {
		util.ShowJson(*out, true)
	}
	return
}

func actionUsageTemplate(params []string) string {
	sort.Strings(params)

	for i := 0; i < len(params); i++ {
		params[i] = "  --" + params[i]
	}

	return fmt.Sprintf(`Usage:{{if .Runnable}}
  {{.CommandPath}} [params]{{end}}{{if .HasExample}}

Examples:
{{.Example}}{{end}}

Available Parameters:
%s

`, strings.Join(params, "\n"))
}
