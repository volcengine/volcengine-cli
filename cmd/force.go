// force.go 实现 ---force 强制泛化调用。
//
// 正常模式下 CLI 仅允许 metadata 中已收录的 service/action；---force 跳过该校验，
// 直接通过 SDK 发起 RPC 请求，适用于未收录产品、新发布接口或需覆盖 API 版本的场景。
//
// 调用入口（维护时按此排查）：
//   1. cmd_root.Execute       -> tryExecuteGenericInvoke：metadata 中不存在的 service
//   2. cmd_service.runServiceCmd：已知 service、action 未匹配子命令时
//   3. cmd_action action RunE：已知 action 子命令且带 ---force（可覆盖 ---version/---method）
//
// 固定参数（三横线，存入 fixedFlags）：
//   ---force   开关，单独出现即 true
//   ---version API 版本；未收录 service 时 force 模式必填，已收录 service 可回落元数据（非 CLI 的 ve -v/--version）
//   ---endpoint 可选；未指定时 invocation 层请求 standard resolver（忽略 profile endpoint）
//   ---method   可选；GET/POST，未指定时优先用已收录 action 元数据，否则默认 GET
package cmd

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/volcengine/volcengine-cli/util"
)

// errNotGenericInvoke 表示当前参数不应走未知 service 兜底路径，应交给 cobra 正常分发。
var errNotGenericInvoke = errors.New("not a generic force invoke")

// builtinRootCommands 非 API 调用的根级子命令；这些名称不能当作 service 解析。
var builtinRootCommands = map[string]struct{}{
	"configure":    {},
	"login":        {},
	"logout":       {},
	"sso":          {},
	"completion":   {},
	"version":      {},
	"enable-color": {},
	"disable-color": {},
}

const fixedFlagsHelp = `  ---profile string    Use a configured profile only for this invocation.
  ---region string     Override the region only for this invocation.
  ---endpoint string   Override the endpoint only for this invocation.
  ---version string    API version; uses metadata when omitted (required with ---force for unlisted services).
  ---method string     HTTP method GET or POST; explicit value overrides metadata, else metadata, else GET.
  ---force             Skip service/action metadata validation and force the call.`

// resetInvocationContext 清空单次调用的 flag 集合。
// ctx 为进程级全局变量，每次解析参数前必须重置，避免多次调用 flag 残留。
func resetInvocationContext() {
	if ctx == nil {
		return
	}
	ctx.fixedFlags = NewFlagSet()
	ctx.dynamicFlags = NewFlagSet()
	ctx.useStandardEndpointResolver = false
}

func parseInvocationArgs(args []string) ([]string, error) {
	resetInvocationContext()
	parser := NewParser(args)
	return parser.ReadArgs(ctx)
}

func parseInvocationFlags(args []string) error {
	_, err := parseInvocationArgs(args)
	return err
}

// isForceEnabled 判断 ---force 是否生效。---force 为开关型固定参数，缺省值视为 true。
func isForceEnabled(c *Context) bool {
	if c == nil || c.fixedFlags == nil {
		return false
	}
	f := c.fixedFlags.GetByName("force")
	if f == nil {
		return false
	}
	v := strings.ToLower(strings.TrimSpace(f.GetValue()))
	return v == "" || v == "true" || v == "1" || v == "yes"
}

// apiVersionForCall 返回本次调用使用的 API 版本：优先 ---version，否则回落到内置元数据。
func apiVersionForCall(c *Context, serviceName string) string {
	if v := forceAPIVersion(c); v != "" {
		return v
	}
	return rootSupport.GetVersion(serviceName)
}

// forceAPIVersion 读取 ---version，即 OpenAPI 接口版本（写入 SDK ClientInfo.Version）。
func forceAPIVersion(c *Context) string {
	if c == nil || c.fixedFlags == nil {
		return ""
	}
	f := c.fixedFlags.GetByName("version")
	if f == nil {
		return ""
	}
	return strings.TrimSpace(f.GetValue())
}

// validateForceCall 校验 force 模式最低要求：---force 已启用，且能解析到 API 版本。
// 已收录 service 可回落元数据版本（对齐阿里云已知 product + --force）；未收录 service 须显式 ---version。
func validateForceCall(c *Context, serviceName string) error {
	if !isForceEnabled(c) {
		return fmt.Errorf("---force is required for force invocation")
	}
	if apiVersionForCall(c, serviceName) == "" {
		return fmt.Errorf("---version is required when using ---force for service %q", serviceName)
	}
	return nil
}

// explicitHTTPMethod 读取显式指定的 ---method，仅允许 GET/POST；空串表示未指定、由元数据或默认值决定。
func explicitHTTPMethod(c *Context) (string, error) {
	if c == nil || c.fixedFlags == nil {
		return "", nil
	}
	f := c.fixedFlags.GetByName("method")
	if f == nil {
		return "", nil
	}
	method := strings.ToUpper(strings.TrimSpace(f.GetValue()))
	if method == "" {
		return "", nil
	}
	if method != "GET" && method != "POST" {
		return "", fmt.Errorf("---method value %q is not supported, please set method in {GET|POST}", method)
	}
	return method, nil
}

// resolveCallStyle 决定调用的 Method/ContentType：method 走 resolveActionHTTPMethod，contentType 取自元数据。
func resolveCallStyle(ctx *Context, serviceName, action string) (method, contentType string, err error) {
	apiInfo := rootSupport.GetApiInfo(serviceName, action)
	method, err = resolveActionHTTPMethod(ctx, apiInfo)
	if err != nil {
		return "", "", err
	}
	if apiInfo != nil && apiInfo.ContentType != "" {
		contentType = apiInfo.ContentType
	}
	return method, contentType, nil
}

func explicitEndpointFromContext(c *Context) string {
	if c == nil || c.fixedFlags == nil {
		return ""
	}
	f := c.fixedFlags.GetByName("endpoint")
	if f == nil {
		return ""
	}
	return strings.TrimSpace(f.GetValue())
}

// dispatchServiceAction 统一 service 级 action 分发：force 优先，否则已知 action 走正常路径。
func dispatchServiceAction(ctx *Context, svc, action string, known bool) error {
	if isForceEnabled(ctx) {
		return doForceAction(ctx, svc, action)
	}
	if known {
		return doAction(ctx, svc, action)
	}
	return fmt.Errorf("%q is not a supported action of %q", action, svc)
}

// buildForceInput 将双横线 API 参数（dynamicFlags）组装为请求体/查询参数。
// 固定参数已在 parser 层归入 fixedFlags，不会进入此处。
func buildForceInput(c *Context) map[string]interface{} {
	input := make(map[string]interface{})
	if c == nil || c.dynamicFlags == nil {
		return input
	}
	for _, f := range c.dynamicFlags.flags {
		if f == nil || f.Name == "" {
			continue
		}
		val := f.GetValue()
		if a, ok := util.ParseToJsonArrayOrObject(strings.TrimSpace(val)); ok {
			input[f.Name] = a
		} else {
			input[f.Name] = val
		}
	}
	return input
}

// doForceAction 执行强制泛化调用：校验参数后委托 executeInvocation。
func doForceAction(ctx *Context, serviceName, action string) error {
	if err := validateForceCall(ctx, serviceName); err != nil {
		return err
	}

	version := apiVersionForCall(ctx, serviceName)
	method, contentType, err := resolveCallStyle(ctx, serviceName, action)
	if err != nil {
		return err
	}

	return executeInvocation(ctx, invocationParams{
		serviceName:                 serviceName,
		action:                      action,
		version:                     version,
		method:                      method,
		contentType:                 contentType,
		useStandardEndpointResolver: explicitEndpointFromContext(ctx) == "",
	}, func() (invocationInput, error) {
		return invocationInput{value: buildForceInput(ctx)}, nil
	})
}

func isKnownServiceName(name string) bool {
	if rootSupport.IsValidSvc(name) {
		return true
	}
	for _, v := range compatible_support_cmd {
		if v == name {
			return true
		}
	}
	return false
}

func isBuiltinRootCommand(name string) bool {
	_, ok := builtinRootCommands[name]
	return ok
}

func argsContainHelp(args []string) bool {
	for _, a := range args {
		if a == "-h" || a == "--help" {
			return true
		}
	}
	return false
}

func printUnknownServiceHelp(serviceName string) error {
	_, err := fmt.Fprintf(os.Stdout, `Usage:
  ve %s <action> [--Param value ...] [fixed flags]

"%s" is not bundled in local metadata. Use ---force with ---version to call it.

Examples:
  ve %s DescribeNewResource ---version 2024-01-01 ---region cn-beijing ---force
  ve %s DescribeNewResource ---version 2024-01-01 ---endpoint open.volcengineapi.com --SomeParam value ---force

Fixed Flags:
%s
`, serviceName, serviceName, serviceName, serviceName, fixedFlagsHelp)
	return err
}

// tryExecuteGenericInvoke 在 cobra 分发前处理 metadata 未收录的 service。
// 返回 errNotGenericInvoke 表示应继续走 cobra；其它 error 为真实失败或已成功执行。
func tryExecuteGenericInvoke(args []string) error {
	if len(args) == 0 {
		return errNotGenericInvoke
	}
	if args[0] == "-h" || args[0] == "--help" {
		return errNotGenericInvoke
	}
	if isBuiltinRootCommand(args[0]) {
		return errNotGenericInvoke
	}
	if isKnownServiceName(args[0]) {
		return errNotGenericInvoke
	}

	serviceName := args[0]
	if argsContainHelp(args[1:]) || len(args) < 2 {
		return printUnknownServiceHelp(serviceName)
	}

	positional, err := parseInvocationArgs(args[1:])
	if err != nil {
		return err
	}
	if len(positional) == 0 {
		return fmt.Errorf("unknown service %q: specify an action name", serviceName)
	}
	action := positional[0]

	if !isForceEnabled(ctx) {
		return fmt.Errorf("unknown service %q: use ---force with ---version to call unlisted APIs", serviceName)
	}

	return doForceAction(ctx, serviceName, action)
}