// force.go 实现 --force 强制泛化调用。
//
// 正常模式下 CLI 仅允许 metadata 中已收录的 service/action；--force 跳过该校验，
// 直接通过 SDK 发起 RPC 请求，适用于未收录产品、新发布接口或需覆盖 API 版本的场景。
//
// 调用入口（维护时按此排查）：
//  1. cmd_root.Execute       -> tryExecuteGenericInvoke：metadata 中不存在的 service
//  2. cmd_service.runServiceCmd：已知 service、action 未匹配子命令时
//  3. cmd_action action RunE：已知 action 子命令且带 --force（可覆盖 --version/--method）
//
// 共享调用前处理见 invocation.go；profile/endpoint 解析真相见 sdk_client.go
// （selectInvocationProfile / resolveClientEndpoint / hasEffectiveFixedEndpoint）。
//
// 系统参数：parser.go publicSystemFlags / localizedSystemFlagsHelp（对外双横线；三横线为冲突逃逸）。
// 双横线保留控制参数：reserved_dynamic.go（--header / --body / --api-param）。
// force 路径额外约定：
//
//	--force     纯开关，出现即启用（只放宽 service/action 元数据校验，不改变 endpoint 解析）
//	--version   未收录 service 时必填；已收录可回落元数据
//	--endpoint  与正常调用同一套解析（flag > resolver=standard > profile/env > 已收录 SDK 解析）；
//	            未收录需要最终生效的固定 host（standard / auto-addressing 不够）
//	--method    可选；未指定时优先元数据，否则 GET
//	--header    可重复 HTTP 头 Name=Value（Content-Type 可覆盖元数据；不进请求体）
//	--body      JSON 请求体；与 flattened 业务参数互斥
//	--api-param 可重复 Name=Value；仅 force 路径将其显式展开为业务参数，
//	            用于未知接口的 query/output 等系统同名参数
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

// isForceEnabled 判断 --force / ---force 是否生效。force 为纯开关，出现即启用。
func isForceEnabled(c *Context) bool {
	if c == nil || c.fixedFlags == nil {
		return false
	}
	return c.fixedFlags.GetByName("force") != nil
}

// validateForceCall 校验 force 模式最低要求：--force 已启用，且能解析到 API 版本。
// 已收录 service 可回落元数据版本；endpoint 与正常调用相同。
// 未收录 service 须有 --version，且最终会生效一个固定 host
// （--endpoint 或 profile/env endpoint；endpoint_resolver=standard / auto-addressing 不算固定 host）。
func validateForceCall(c *Context, serviceName string) error {
	if !isForceEnabled(c) {
		return trErrorf("--force is required for force invocation")
	}
	if err := validateProfileIfSpecified(c); err != nil {
		return err
	}
	if apiVersionForCall(c, serviceName) == "" {
		return trErrorf("--version is required when using --force for service %q", serviceName)
	}
	if !rootSupport.IsValidSvc(serviceName) && !hasEffectiveFixedEndpoint(c) {
		return trErrorf("endpoint is required for unlisted service %q: set --endpoint, or configure endpoint in the profile / VOLCENGINE_ENDPOINT (endpoint-resolver=standard alone is not enough)", serviceName)
	}
	return nil
}

// buildForceInput 将双横线 API 参数（dynamicFlags）组装为请求体/查询参数。
// 固定参数已在 parser 层归入 fixedFlags；--header/--body/--api-param 等保留动态 flag 不会进入此处。
func buildForceInput(c *Context) map[string]interface{} {
	input := make(map[string]interface{})
	if c == nil || c.dynamicFlags == nil {
		return input
	}
	for _, f := range c.dynamicFlags.flags {
		if f == nil || f.Name == "" || isSkipBodyDynamicFlag(f.Name) {
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

// expandForceAPIParams expands repeatable --api-param Name=Value controls into
// ordinary dynamic flags. It is deliberately called only from the force path:
// outside --force, the control is rejected instead of being silently ignored.
//
// Names are trimmed (while preserving case), values are preserved verbatim and
// split only on the first '='. Exact-name duplicates -- including a collision
// with a directly supplied --Name value -- are rejected so no value wins
// implicitly. Reserved CLI control names cannot be smuggled into the request.
func expandForceAPIParams(ctx *Context) error {
	if ctx == nil || ctx.dynamicFlags == nil {
		return nil
	}
	escape := ctx.dynamicFlags.GetByName("api-param")
	if escape == nil {
		return nil
	}

	type apiParam struct {
		name  string
		value string
	}
	params := make([]apiParam, 0, len(escape.GetValues()))
	seen := make(map[string]struct{}, len(escape.GetValues()))
	for _, raw := range escape.GetValues() {
		idx := strings.IndexByte(raw, '=')
		if idx < 0 {
			return trErrorf("invalid --api-param %q, expected Name=Value", raw)
		}
		name := strings.TrimSpace(raw[:idx])
		if name == "" {
			return trErrorf("invalid --api-param %q, parameter name must not be empty", raw)
		}
		if isReservedDynamicFlag(name) {
			return trErrorf("--api-param name %q is reserved by the CLI", name)
		}
		if _, exists := seen[name]; exists {
			return trErrorf("--api-param parameter %q cannot be specified more than once", name)
		}
		if ctx.dynamicFlags.GetByName(name) != nil {
			return trErrorf("--api-param parameter %q conflicts with direct --%s", name, name)
		}
		seen[name] = struct{}{}
		params = append(params, apiParam{name: name, value: raw[idx+1:]})
	}

	for _, param := range params {
		flag, err := ctx.dynamicFlags.AddByName(param.name)
		if err != nil {
			return err
		}
		flag.SetValue(param.value)
	}
	return nil
}

// doForceAction 执行强制泛化调用：校验参数后委托 executeInvocation。
func doForceAction(ctx *Context, serviceName, action string) error {
	if err := validateForceCall(ctx, serviceName); err != nil {
		return err
	}
	if err := expandForceAPIParams(ctx); err != nil {
		return err
	}

	version := apiVersionForCall(ctx, serviceName)
	method, contentType, headers, err := resolveCallStyle(ctx, serviceName, action)
	if err != nil {
		return err
	}

	return executeInvocation(ctx, invocationParams{
		serviceName: serviceName,
		action:      action,
		version:     version,
		method:      method,
		contentType: contentType,
		headers:     headers,
	}, func() (invocationInput, error) {
		return buildForceInvocationInput(ctx, serviceName, action, contentType)
	})
}

// buildForceInvocationInput 组装 force 路径请求体。
// 有 ApiMeta 时与 doAction 相同，一律走 buildActionInput（含 isStringParam / --body 语义）；
// 无元数据时同样走 buildActionInput（支持 --body 与扁平参数），不再只用 buildForceInput。
func buildForceInvocationInput(ctx *Context, serviceName, action, contentType string) (invocationInput, error) {
	jsonBody := isJSONContentType(contentType)
	var flags []*Flag
	if ctx != nil && ctx.dynamicFlags != nil {
		flags = ctx.dynamicFlags.flags
	}
	apiMeta := rootSupport.GetApiMeta(serviceName, action)
	input, fromBody, err := buildActionInput(flags, apiMeta, jsonBody)
	if err != nil {
		return invocationInput{}, err
	}
	// When neither --body nor flat flags produced structured JSON but Content-Type
	// is JSON, still return an empty map rather than nil for SDK stability.
	if input == nil && jsonBody {
		input = map[string]interface{}{}
	}
	return invocationInput{value: input, jsonBody: jsonBody, fromBody: fromBody}, nil
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

// argsContainHelp 判断参数列表中是否出现独立的 -h / --help 帮助开关。
// 与 parseActionHelpArgs 相同：前一个待取值 flag 的值不会被当成帮助开关。
func argsContainHelp(args []string) bool {
	wantHelp, _ := parseActionHelpArgs(args)
	return wantHelp
}

// printUnknownServiceHelp 打印未收录 service 的用法提示（要求 --force / --version / 固定 endpoint）。
// System Flags 与 root/service/action usage 共用 localizedSystemFlagsHelp。
func printUnknownServiceHelp(serviceName string) error {
	_, err := fmt.Fprintf(os.Stdout, `%s
  ve %s <action> [--Param value ...] [system flags]

%s

%s
  ve %s DescribeNewResource --version 2024-01-01 --region cn-beijing --endpoint newservice.cn-beijing.volcengineapi.com --force
  ve %s DescribeNewResource --version 2024-01-01 --endpoint open.volcengineapi.com --SomeParam value --force

%s
%s
`, tr("Usage:"), serviceName, trf(`"%s" is not bundled in local metadata. Use --force with --version, and a fixed endpoint via --endpoint or profile/VOLCENGINE_ENDPOINT (endpoint-resolver=standard alone is not enough).`, serviceName), tr("Examples:"), serviceName, serviceName, tr("System Flags:"), localizedSystemFlagsHelp())
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
		return trErrorf("unknown service %q: specify an action name", serviceName)
	}
	action := positional[0]

	if !isForceEnabled(ctx) {
		return trErrorf("unknown service %q: use --force with --version, and a fixed endpoint via --endpoint or profile/VOLCENGINE_ENDPOINT (endpoint-resolver=standard alone is not enough)", serviceName)
	}

	return doForceAction(ctx, serviceName, action)
}
