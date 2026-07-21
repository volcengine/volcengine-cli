// force.go 实现 ---force 强制泛化调用。
//
// 正常模式下 CLI 仅允许 metadata 中已收录的 service/action；---force 跳过该校验，
// 直接通过 SDK 发起 RPC 请求，适用于未收录产品、新发布接口或需覆盖 API 版本的场景。
//
// 调用入口（维护时按此排查）：
//  1. cmd_root.Execute       -> tryExecuteGenericInvoke：metadata 中不存在的 service
//  2. cmd_service.runServiceCmd：已知 service、action 未匹配子命令时
//  3. cmd_action action RunE：已知 action 子命令且带 ---force（可覆盖 ---version/---method）
//
// 固定参数（三横线）的白名单与 Usage 文案见 parser.go（allowedFixedFlags / fixedFlagsHelp）。
// force 路径额外约定：
//
//	---force    纯开关，出现即启用（只放宽 service/action 元数据校验，不改变 endpoint 解析）
//	---version  未收录 service 时必填；已收录可回落元数据
//	---endpoint 与正常调用同一套解析（flag > resolver=standard > profile/env > 已收录 SDK 解析）；
//	            未收录需要最终生效的固定 host（standard / auto-addressing 不够）
//	---method   可选；未指定时优先元数据，否则 GET
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

// resetInvocationContext 清空单次调用的 flag 集合。
// ctx 为进程级全局变量，每次解析参数前必须重置，避免多次调用 flag 残留。
func resetInvocationContext() {
	if ctx == nil {
		return
	}
	ctx.fixedFlags = NewFlagSet()
	ctx.dynamicFlags = NewFlagSet()
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

// isForceEnabled 判断 ---force 是否生效。---force 为纯开关，出现即启用。
func isForceEnabled(c *Context) bool {
	if c == nil || c.fixedFlags == nil {
		return false
	}
	return c.fixedFlags.GetByName("force") != nil
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
// 已收录 service 可回落元数据版本；endpoint 与正常调用相同。
// 未收录 service 须有 ---version，且最终会生效一个固定 host
// （---endpoint 或 profile/env endpoint；endpoint_resolver=standard / auto-addressing 不算固定 host）。
func validateForceCall(c *Context, serviceName string) error {
	if !isForceEnabled(c) {
		return fmt.Errorf("---force is required for force invocation")
	}
	if err := validateProfileIfSpecified(c); err != nil {
		return err
	}
	if apiVersionForCall(c, serviceName) == "" {
		return fmt.Errorf("---version is required when using ---force for service %q", serviceName)
	}
	if !rootSupport.IsValidSvc(serviceName) && !hasEffectiveFixedEndpoint(c) {
		return fmt.Errorf("endpoint is required for unlisted service %q: set ---endpoint, or configure endpoint in the profile / VOLCENGINE_ENDPOINT (endpoint-resolver=standard alone is not enough)", serviceName)
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

func isAutoAddressingEndpoint(s string) bool {
	return strings.ToLower(strings.TrimSpace(s)) == "auto-addressing"
}

// resolveInvocationProfileName 与 NewSimpleClient 一致：---profile > Current > env。
func resolveInvocationProfileName(c *Context) string {
	if c == nil {
		return ""
	}
	name := ""
	if c.config != nil {
		name, _ = defaultProfileNameWithSource(c.config)
	}
	if c.fixedFlags != nil {
		if f := c.fixedFlags.GetByName("profile"); f != nil {
			if v := strings.TrimSpace(f.GetValue()); v != "" {
				name = v
			}
		}
	}
	return name
}

// validateProfileIfSpecified 在指定 ---profile 时与 NewSimpleClient 一致地校验 profile 存在。
func validateProfileIfSpecified(c *Context) error {
	if c == nil || c.fixedFlags == nil || c.config == nil {
		return nil
	}
	f := c.fixedFlags.GetByName("profile")
	if f == nil {
		return nil
	}
	name := strings.TrimSpace(f.GetValue())
	if name == "" {
		return nil
	}
	if c.config.Profiles[name] == nil {
		return fmt.Errorf("profile %q not found", name)
	}
	return nil
}

// profileOrEnvEndpoint 读取与 NewSimpleClient 一致的 profile/env endpoint 字符串（不含 ---endpoint）。
// profile 存在但 endpoint 为空时回落 VOLCENGINE_ENDPOINT。
func profileOrEnvEndpoint(c *Context) string {
	if c != nil && c.config != nil {
		name := resolveInvocationProfileName(c)
		if name != "" {
			if p := c.config.Profiles[name]; p != nil {
				if e := strings.TrimSpace(p.Endpoint); e != "" {
					return e
				}
			}
		}
	}
	return strings.TrimSpace(os.Getenv("VOLCENGINE_ENDPOINT"))
}

// profileOrEnvEndpointResolver 读取 profile/env 的 endpoint-resolver（不含被 ---endpoint 清空的情况）。
func profileOrEnvEndpointResolver(c *Context) string {
	if c != nil && c.config != nil {
		name := resolveInvocationProfileName(c)
		if name != "" {
			if p := c.config.Profiles[name]; p != nil {
				if r := strings.TrimSpace(p.EndpointResolver); r != "" {
					return strings.ToLower(r)
				}
			}
		}
	}
	return strings.ToLower(strings.TrimSpace(os.Getenv("VOLCENGINE_ENDPOINT_RESOLVER")))
}

// hasEffectiveFixedEndpoint 判断按 NewSimpleClient 规则是否会落到固定 host（非 standard resolver）。
// 与 client 对齐：---endpoint 优先并清空 resolver；resolver=standard 时忽略 profile/env host；
// auto-addressing 视为走 standard resolver，对未收录 service 不算可用固定 host。
func hasEffectiveFixedEndpoint(c *Context) bool {
	if ep := explicitEndpointFromContext(c); ep != "" {
		return !isAutoAddressingEndpoint(ep)
	}
	if profileOrEnvEndpointResolver(c) == "standard" {
		return false
	}
	ep := profileOrEnvEndpoint(c)
	return ep != "" && !isAutoAddressingEndpoint(ep)
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
		serviceName: serviceName,
		action:      action,
		version:     version,
		method:      method,
		contentType: contentType,
	}, func() (invocationInput, error) {
		return buildForceInvocationInput(ctx, serviceName, action, contentType)
	})
}

// buildForceInvocationInput 组装 force 路径请求体；有元数据的 application/json action 复用 buildActionInput。
func buildForceInvocationInput(ctx *Context, serviceName, action, contentType string) (invocationInput, error) {
	jsonBody := strings.ToLower(contentType) == "application/json"
	if jsonBody {
		if apiMeta := rootSupport.GetApiMeta(serviceName, action); apiMeta != nil {
			input, fromBody, err := buildActionInput(ctx.dynamicFlags.flags, apiMeta, jsonBody)
			if err != nil {
				return invocationInput{}, err
			}
			return invocationInput{value: input, jsonBody: jsonBody, fromBody: fromBody}, nil
		}
	}
	return invocationInput{value: buildForceInput(ctx), jsonBody: jsonBody, fromBody: false}, nil
}

// isRegisteredRootSubcommand 判断 name 是否已是 root 子命令（builtin / metadata service / 兼容别名）。
// 以 rootCmd 注册表为唯一来源，避免手写名单与 AddCommand 双源不同步。
// 注意：help/version 等在 initRootCmd 中注册，tryExecuteGenericInvoke 须在 initRootCmd 之后调用。
func isRegisteredRootSubcommand(name string) bool {
	if name == "" {
		return false
	}
	for _, c := range rootCmd.Commands() {
		if c.Name() == name {
			return true
		}
		for _, alias := range c.Aliases {
			if alias == name {
				return true
			}
		}
	}
	return false
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

"%s" is not bundled in local metadata. Use ---force with ---version, and a fixed endpoint via ---endpoint or profile/VOLCENGINE_ENDPOINT (endpoint-resolver=standard alone is not enough).

Examples:
  ve %s DescribeNewResource ---version 2024-01-01 ---region cn-beijing ---endpoint newservice.cn-beijing.volcengineapi.com ---force
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
	// 根级 flag（含 -v/--version、未知 --foo）交给 cobra；不与 ---version（API 版本）混淆。
	if strings.HasPrefix(args[0], "-") {
		return errNotGenericInvoke
	}
	// 已注册根命令（builtin + service + 兼容别名）一律交给 cobra；仅未注册名走 force。
	if isRegisteredRootSubcommand(args[0]) {
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
		return fmt.Errorf("unknown service %q: use ---force with ---version, and a fixed endpoint via ---endpoint or profile/VOLCENGINE_ENDPOINT (endpoint-resolver=standard alone is not enough)", serviceName)
	}

	return doForceAction(ctx, serviceName, action)
}
